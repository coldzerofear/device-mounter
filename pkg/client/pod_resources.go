package client

import (
	"net"
	"os"
	"sync"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"k8s.io/klog/v2"
	"k8s.io/kubelet/pkg/apis/podresources/v1alpha1"
)

const (
	SocketDir      = "/var/lib/kubelet/pod-resources"
	SocketPath     = SocketDir + "/kubelet.sock"
	DefaultTimeout = 10 * time.Second
)

type PodResourcesClientPorxy struct {
	lock         sync.RWMutex // 用于保护conn的并发访问
	conn         *grpc.ClientConn
	podResources v1alpha1.PodResourcesListerClient
	socketPath   string
	stopCh       chan struct{}
}

var (
	onceInitPodResClient sync.Once
	podResourcesClient   *PodResourcesClientPorxy
)

func initClientConn(socketPath string) (*grpc.ClientConn, error) {
	_, err := os.Stat(socketPath)
	if os.IsNotExist(err) {
		return nil, err
	}
	return Dial(socketPath, DefaultTimeout)
}

func newPodResourcesClientPorxy(socketPath string) (*PodResourcesClientPorxy, error) {
	conn, err := initClientConn(socketPath)
	if err != nil {
		return nil, err
	}
	proxy := &PodResourcesClientPorxy{
		socketPath:   socketPath,
		conn:         conn,
		podResources: v1alpha1.NewPodResourcesListerClient(conn),
		stopCh:       make(chan struct{}, 1),
	}
	go proxy.monitorConnection()
	return proxy, nil
}

func (p *PodResourcesClientPorxy) GetClient() v1alpha1.PodResourcesListerClient {
	p.lock.RLock()
	defer p.lock.RUnlock()

	return p.podResources
}

func (p *PodResourcesClientPorxy) Close() error {
	p.lock.Lock()
	defer p.lock.Unlock()
	// 停止监控连接
	p.stopCh <- struct{}{}
	return p.conn.Close()
}

func (p *PodResourcesClientPorxy) monitorConnection() {
outer:
	for {
		select {
		case <-p.stopCh:
			klog.Warningln("Stopping monitoring connection")
			break outer
		default:
		}
		state := p.conn.GetState()
		switch state {
		case connectivity.Ready:
			// 连接正常，休眠一段时间后继续检查
			time.Sleep(5 * time.Second)
		case connectivity.Idle, connectivity.Connecting:
			// 等待连接建立，这里也可以选择做日志记录或轻微的重试间隔
			time.Sleep(1 * time.Second)
		case connectivity.TransientFailure, connectivity.Shutdown:
			// 连接失败或关闭，尝试重新连接
			klog.Infoln("Connection lost, attempting to reconnect...")
			if err := p.recoverGRPCConnection(); err != nil {
				klog.Errorf("Reconnection failed: %v", err)
			} else {
				klog.Warningln("Reconnected successfully.")
			}
		}
	}
}

func (p *PodResourcesClientPorxy) recoverGRPCConnection() error {
	p.lock.Lock()
	defer p.lock.Unlock()

	// 关闭旧连接（如果还存在）
	if p.conn != nil {
		if err := p.conn.Close(); err != nil {
			return errors.Wrap(err, "failed to close old connection")
		}
	}
	// 尝试重新建立连接
	newConn, err := initClientConn(p.socketPath)
	if err != nil {
		return errors.Wrap(err, "failed to redial")
	}
	p.conn = newConn
	p.podResources = v1alpha1.NewPodResourcesListerClient(p.conn)
	return nil
}

func GetPodResourcesClinet() *PodResourcesClientPorxy {
	onceInitPodResClient.Do(func() {
		var err error
		podResourcesClient, err = newPodResourcesClientPorxy(SocketPath)
		if err != nil {
			klog.Exitf("Pod Resources Client initialization failed: %v", err)
		}
	})
	return podResourcesClient
}

func Dial(unixSocketPath string, timeout time.Duration) (*grpc.ClientConn, error) {
	if c, err := grpc.Dial(unixSocketPath,
		grpc.WithInsecure(), grpc.WithBlock(), grpc.WithTimeout(timeout),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}),
	); err != nil {
		return nil, err
	} else {
		return c, nil
	}
}

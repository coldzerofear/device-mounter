package mounter

import (
	"log"
	"net"
	"os"
	"testing"
	"time"

	"github.com/coldzerofear/device-mounter/pkg/api"
	"google.golang.org/grpc"
)

func TestDeviceMounterServer(t *testing.T) {
	t.Run("Test tcp server", func(t *testing.T) {
		listen, err := net.Listen("tcp", ":1200")
		if err != nil {
			t.Errorf("Failed to listen: %v", err)
		}
		defer listen.Close()
		s := grpc.NewServer()
		api.RegisterDeviceMountServiceServer(s, &DeviceMounterImpl{})
		log.Println("Serving ...")
		go func() {
			if err = s.Serve(listen); err != nil {
				t.Errorf("Failed to serve: %v", err)
			}
		}()
		defer s.GracefulStop()
		time.Sleep(5 * time.Second)
		log.Println("DeviceMounterServer start successfully")
	})
}

func TestDeviceMounterUnixServer(t *testing.T) {
	t.Run("Test unix server", func(t *testing.T) {
		var socketPath = "./test.sock"
		os.RemoveAll(socketPath)
		addr, err := net.ResolveUnixAddr("unix", socketPath)
		if err != nil {
			log.Fatalf("fialed to resolve unix addr: %v", err)
		}
		listen, err := net.ListenUnix("unix", addr)
		if err != nil {
			log.Fatalf("Failed to listen: %v", err)
		}
		defer func() {
			_ = listen.Close()
			_ = os.RemoveAll(socketPath)
		}()
		s := grpc.NewServer()
		api.RegisterDeviceMountServiceServer(s, &DeviceMounterImpl{})
		log.Println("Serving ...")
		go func() {
			if err = s.Serve(listen); err != nil {
				t.Errorf("Failed to serve: %v", err)
			}
		}()
		defer s.GracefulStop()
		time.Sleep(5 * time.Second)
		log.Println("DeviceMounterUnixServer start successfully")
	})
}

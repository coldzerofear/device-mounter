package tlsconfig

import (
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"k8s-device-mounter/pkg/api/v1alpha1"
	"k8s-device-mounter/pkg/authConfig"
	"k8s-device-mounter/pkg/filewatch"
	"k8s.io/klog/v2"

	ocpconfigv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/crypto"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/util/cert"
)

type Watch interface {
	AddToFilewatch(watch filewatch.Watch) error
	Reload()
	GetConfig() (*tls.Config, error)
}

func NewWatch(configDir, tlsProfileFileName, certAndKeyDir, certsName, keyName string, authConfig authConfig.Reader) Watch {
	return &watch{
		configDir:          configDir,
		tlsProfileFileName: tlsProfileFileName,
		tlsProfileError:    fmt.Errorf("tls profile not loaded"),

		certAndKeyDir: certAndKeyDir,
		certsName:     certsName,
		keyName:       keyName,
		certError:     fmt.Errorf("certificate not loaded"),

		authConfig: authConfig,
	}
}

type watch struct {
	lock sync.RWMutex

	configDir          string
	tlsProfileFileName string

	certAndKeyDir string
	certsName     string
	keyName       string

	tlsProfileError error
	ciphers         []uint16
	minTlsVersion   uint16

	certificate *tls.Certificate
	certError   error

	authConfig authConfig.Reader
}

func (w *watch) AddToFilewatch(watch filewatch.Watch) error {
	if err := watch.Add(w.configDir, w.reloadTlsProfile); err != nil {
		return err
	}
	return watch.Add(w.certAndKeyDir, w.reloadCertificate)
}

func (w *watch) Reload() {
	w.reloadTlsProfile()
	w.reloadCertificate()
}

func (w *watch) GetConfig() (*tls.Config, error) {
	clientCa, err := w.authConfig.GetClientCA()
	if err != nil {
		return nil, err
	}

	w.lock.RLock()
	defer w.lock.RUnlock()

	if w.tlsProfileError != nil {
		return nil, w.tlsProfileError
	}
	if w.certError != nil {
		return nil, w.certError
	}

	return &tls.Config{
		CipherSuites: w.ciphers,
		MinVersion:   w.minTlsVersion,
		Certificates: []tls.Certificate{*w.certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCa,
	}, nil
}

func (w *watch) reloadTlsProfile() {
	w.lock.Lock()
	defer w.lock.Unlock()
	w.tlsProfileError = nil

	ciphers, minVersion, err := loadCipherSuitesAndMinVersion(filepath.Join(w.configDir, w.tlsProfileFileName))
	if errors.Is(err, os.ErrNotExist) {
		// Config file does not exist, using zero values for default
		w.ciphers = nil
		w.minTlsVersion = 0
		return
	}
	if err != nil {
		klog.Errorf("Failed to load TLS configuration: %s", err)
		w.tlsProfileError = fmt.Errorf("failed to load TLS configuration: %w", err)
		return
	}

	klog.Infof("Loaded TLS configuration.")
	{
		// TODO: only compute human readable strings on debug level.
		// For now, there is no easy way to test for logging level.
		cipherNames := crypto.CipherSuitesToNamesOrDie(ciphers)
		minVersionName := crypto.TLSVersionToNameOrDie(minVersion)
		klog.V(1).Infof("Set min TLS version: %s", minVersionName)
		klog.V(1).Infof("Set ciphers: %s", strings.Join(cipherNames, ", "))
	}

	w.ciphers = ciphers
	w.minTlsVersion = minVersion
}

func (w *watch) reloadCertificate() {
	w.lock.Lock()
	defer w.lock.Unlock()
	w.certError = nil

	certificate, err := LoadCertificates(
		filepath.Join(w.certAndKeyDir, w.certsName),
		filepath.Join(w.certAndKeyDir, w.keyName),
	)
	if err != nil {
		w.certError = err
		return
	}

	klog.Infof("Loaded TLS certificate.")
	w.certificate = certificate
}

func loadCipherSuitesAndMinVersion(configPath string) ([]uint16, uint16, error) {
	tlsProfile, err := loadTlsProfile(configPath)
	if err != nil {
		return nil, 0, fmt.Errorf("could not load tls config: %w", err)
	}

	ciphers, minVersion, err := getTlsCiphersAndMinVersion(tlsProfile)
	if err != nil {
		return nil, 0, err
	}

	return ciphers, minVersion, nil
}

func loadTlsProfile(profilePath string) (*v1alpha1.TlsSecurityProfile, error) {
	file, err := os.Open(profilePath)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %w", err)
	}
	// It's ok to ignore error on close, because the file is opened of reading
	defer func() { _ = file.Close() }()

	result := &v1alpha1.TlsSecurityProfile{}
	err = yaml.NewYAMLToJSONDecoder(file).Decode(result)
	if err != nil {
		return nil, fmt.Errorf("error decoding tls config: %w", err)
	}
	return result, nil
}

func getTlsCiphersAndMinVersion(tlsProfile *v1alpha1.TlsSecurityProfile) ([]uint16, uint16, error) {
	var profile *ocpconfigv1.TLSProfileSpec
	if tlsProfile.Type == ocpconfigv1.TLSProfileCustomType {
		if tlsProfile.Custom == nil {
			return nil, 0, fmt.Errorf("tls profile \"custom\" field is nil")
		}
		profile = &tlsProfile.Custom.TLSProfileSpec
	} else {
		var exists bool
		profile, exists = ocpconfigv1.TLSProfiles[tlsProfile.Type]
		if !exists {
			return nil, 0, fmt.Errorf("unknown profile type: %s", tlsProfile.Type)
		}
	}

	ciphers := getCipherSuites(profile)
	minVersion, err := crypto.TLSVersion(string(profile.MinTLSVersion))
	if err != nil {
		return nil, 0, err
	}

	return ciphers, minVersion, nil
}

func getCipherSuites(profileSpec *ocpconfigv1.TLSProfileSpec) []uint16 {
	tlsCiphers := make(map[string]*tls.CipherSuite, len(tls.CipherSuites()))
	for _, suite := range tls.CipherSuites() {
		tlsCiphers[suite.Name] = suite
	}

	cipherIds := make([]uint16, 0, len(profileSpec.Ciphers))
	for _, ianaCipher := range crypto.OpenSSLToIANACipherSuites(profileSpec.Ciphers) {
		if cipher, found := tlsCiphers[ianaCipher]; found {
			cipherIds = append(cipherIds, cipher.ID)
		}
	}

	return cipherIds
}

func LoadCertificates(certPath, keyPath string) (*tls.Certificate, error) {
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}

	crt, err := tls.X509KeyPair(certBytes, keyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to load certificate: %v\n", err)
	}
	leaf, err := cert.ParseCertsPEM(certBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to load leaf certificate: %v\n", err)
	}
	crt.Leaf = leaf[0]
	return &crt, nil
}

package k8s

import (
	"testing"
)

const testKubeconfigWithCA = `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUJkakNDQVIyZ0F3SUJBZ0lCQURBS0JnZ3Foa2pPUFFRREFqQWpNU0V3SHdZRFZRUUREQmhyTTNNdGMyVnkKZG1WeUxXTmhRREUzT0RNNU5UTTFOelV3SGhjTk1qWXdOekV6TVRRek9UTTFXaGNOTXpZd056RXdNVFF6T1RNMQpXakFqTVNFd0h3WURWUVFEREJock0zTXRjMlZ5ZG1WeUxXTmhRREUzT0RNNU5UTTFOelV3V1RBVEJnY3Foa2pPClBRSUJCZ2dxaGtqT1BRTUJCd05DQUFTSFc3RFV4SVltSGFHeDdFU2RzdGh3RnoxWGIrdWhlM2huN0UwaGxLTUEKUWlHbDhJem0vZURWS0ZVNUI1NW40aThNZW5hQ3E5LzJWMlZMemIvcjRoSktvMEl3UURBT0JnTlZIUThCQWY4RQpCQU1DQXFRd0R3WURWUjBUQVFIL0JBVXdBd0VCL3pBZEJnTlZIUTRFRmdRVU5MZng5dnNSMlRadXcwQjErZ3ZCCmZBTTA5eTB3Q2dZSUtvWkl6ajBFQXdJRFJ3QXdSQUlnY3h5VkttNFN3RC96dnNVWFkvaUE0bS9qeXd2YUxqaDUKMVJCbitsejhma2tDSUNkbituMWJrOWlXUkFUWktNeSs2Nk50cUlRKzc0Tzc3R2FsclMrT0VSODQKLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo=
    server: https://192.168.65.3:6443
  name: default
contexts:
- context:
    cluster: default
    user: default
  name: default
current-context: default
kind: Config
users:
- name: default
  user:
    client-certificate-data: e30=
    client-key-data: e30=
`

func TestBuildFromKubeconfig_InsecureClearsCA(t *testing.T) {
	cfg, err := BuildFromKubeconfig([]byte(testKubeconfigWithCA), true)
	if err != nil {
		t.Fatalf("BuildFromKubeconfig: %v", err)
	}
	if !cfg.TLSClientConfig.Insecure {
		t.Fatal("expected Insecure=true")
	}
	if len(cfg.TLSClientConfig.CAData) != 0 || cfg.TLSClientConfig.CAFile != "" {
		t.Fatal("CA must be cleared when Insecure is set")
	}
}

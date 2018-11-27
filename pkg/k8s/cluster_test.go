// kibosh
//
// Copyright (c) 2017-Present Pivotal Software, Inc. All Rights Reserved.
//
// This program and the accompanying materials are made available under the terms of the under the Apache License,
// Version 2.0 (the "License”); you may not use this file except in compliance with the License. You may
// obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software distributed under the
// License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing permissions and
// limitations under the License.

package k8s_test

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/cf-platform-eng/kibosh/pkg/config"
	. "github.com/cf-platform-eng/kibosh/pkg/k8s"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sAPI "k8s.io/client-go/tools/clientcmd/api"
)

var _ = Describe("Config", func() {
	var creds *config.ClusterCredentials

	BeforeEach(func() {
		creds = &config.ClusterCredentials{
			CAData: []byte("c29tZSByYW5kb20gc3R1ZmY="),
			Server: "127.0.0.1/api",
			Token:  "my-token",
		}
	})

	It("list pods", func() {
		var url string
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			url = string(r.URL.Path)
		})
		testserver := httptest.NewServer(handler)
		creds.Server = testserver.URL

		cluster, err := NewCluster(creds)

		Expect(err).To(BeNil())

		cluster.ListPods("mynamespace", meta_v1.ListOptions{})

		Expect(url).To(Equal("/api/v1/namespaces/mynamespace/pods"))
	})

	It("loads default config", func() {
		configFile, err := ioutil.TempFile("", "")
		Expect(err).To(BeNil())

		configFile.Write([]byte(`
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: Zm9v
    server: https://127.0.0.1
  name: my_cluster
contexts:
- context:
    cluster: my_cluster
    user: my_cluster_user
  name: my_cluster
current-context: my_cluster
kind: Config
preferences: {}
users:
- name: my_cluster_user
  user:
    token: cGFzc3dvcmQ=
`))

		os.Setenv("KUBECONFIG", configFile.Name())

		cluster, err := NewClusterFromDefaultConfig()

		Expect(err).To(BeNil())
		clientConfig := cluster.GetClientConfig()
		Expect(clientConfig).NotTo(BeNil())
		Expect(clientConfig.BearerToken).To(Equal("cGFzc3dvcmQ="))
	})

	It("load specific config", func() {
		k8sConfig := &k8sAPI.Config{
			Clusters: map[string]*k8sAPI.Cluster{
				"cluster1": {
					CertificateAuthorityData: []byte("my cat"),
					Server:                   "myserver",
				},
				"cluster2": {
					CertificateAuthorityData: []byte("my cat"),
					Server:                   "myserver",
				},
			},
			CurrentContext: "context2",
			Contexts: map[string]*k8sAPI.Context{
				"context1": {
					Cluster:  "cluster1",
					AuthInfo: "auth1",
				},
				"context2": {
					Cluster:  "cluster2",
					AuthInfo: "auth2",
				},
			},
			AuthInfos: map[string]*k8sAPI.AuthInfo{
				"auth1": {
					Token: "my encoded token",
				},
				"auth2": {
					Token: "my encoded 2nd token",
				},
			},
		}

		cluster, err := GetClusterFromK8sConfig(k8sConfig)

		Expect(err).To(BeNil())

		clientConfig := cluster.GetClientConfig()

		Expect(clientConfig).NotTo(BeNil())
		Expect(clientConfig.BearerToken).To(Equal("my encoded 2nd token"))
	})

	It("no current context", func() {
		k8sConfig := &k8sAPI.Config{
			Clusters: map[string]*k8sAPI.Cluster{
				"cluster1": {
					CertificateAuthorityData: []byte("my cat"),
					Server:                   "myserver",
				},
				"cluster2": {
					CertificateAuthorityData: []byte("my cat"),
					Server:                   "myserver",
				},
			},
			Contexts: map[string]*k8sAPI.Context{
				"context1": {
					Cluster:  "cluster1",
					AuthInfo: "auth1",
				},
				"context2": {
					Cluster:  "cluster2",
					AuthInfo: "auth2",
				},
			},
			AuthInfos: map[string]*k8sAPI.AuthInfo{
				"auth1": {
					Token: "my encoded token",
				},
				"auth2": {
					Token: "my encoded 2nd token",
				},
			},
		}

		_, err := GetClusterFromK8sConfig(k8sConfig)

		Expect(err).NotTo(BeNil())

	})
})

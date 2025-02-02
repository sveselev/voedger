/*
 * Copyright (c) 2021-present unTill Pro, Ltd.
 */

package sys_it

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	coreutils "github.com/voedger/voedger/pkg/utils"
	it "github.com/voedger/voedger/pkg/vit"
)

func TestBasicUsage_ReverseProxy(t *testing.T) {
	require := require.New(t)
	vit := it.NewVIT(t, &it.SharedConfig_App1)
	defer vit.TearDown()

	targetListener, err := net.Listen("tcp", coreutils.ServerAddress(it.TestServicePort))
	require.NoError(err)
	errs := make(chan error)
	targetHandler := targetHandler{t, &sync.Mutex{}, "", ""}
	targetServer := http.Server{
		Handler: &targetHandler,
	}
	go func() {
		errs <- targetServer.Serve(targetListener)
	}()

	body := `world`

	cases := map[string]string{
		"grafana/foo":         "/grafana/foo",
		"grafana/foo/bar/":    "/grafana/foo/bar/",
		"grafana-rewrite/foo": "/rewritten/foo",
		"unknown/foo":         "/not-found/unknown/foo",

		// https://dev.untill.com/projects/#!627070
		"api/untill/airs-bp//c.air.StoreSubscriptionProfile": "/not-found/api/untill/airs-bp//c.air.StoreSubscriptionProfile",
	}

	for srcURL, expectedURLPath := range cases {
		targetHandler.setExpectedURLPath(expectedURLPath)

		resp := vit.PostFree(fmt.Sprintf("http://127.0.0.1:%s/%s", vit.IFederation.URL().Port(), srcURL), body)
		require.Equal(`hello world`, resp.Body) // guarantees that expectedURLPath is checked by the handler
	}

	t.Run("route domain", func(t *testing.T) {
		targetHandler.setExpectedURLPath("/grafana/foo/")
		targetHandler.setExpectedHost("127.0.0.1")
		resp := vit.PostFree(fmt.Sprintf("http://localhost:%s/grafana/foo/?Datadfsdfsdfsdfdf", vit.IFederation.URL().Port()), body)
		require.Equal(`hello world`, resp.Body)
	})

	// stop everything
	require.NoError(targetServer.Shutdown(context.Background()))
	err = <-errs
	if err != http.ErrServerClosed {
		t.Fatal(err)
	}
	targetListener.Close()

}

type targetHandler struct {
	t               *testing.T
	lock            sync.Locker
	expectedURLPath string
	expectedHost    string
}

func (h *targetHandler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	rw.WriteHeader(http.StatusOK)
	body, err := io.ReadAll(req.Body)
	require.NoError(h.t, err)
	req.Close = true
	req.Body.Close()
	if len(h.expectedHost) > 0 {
		require.Contains(h.t, req.Host, h.expectedHost) // check localhost->127.0.0.1 translation by RouteDomains rule
	}
	require.Equal(h.t, h.getExpectedURLPath(), req.URL.Path)
	require.Equal(h.t, http.MethodPost, req.Method, req.URL.Path)
	_, err = rw.Write([]byte("hello " + string(body))) // test will be failed in case of any error
	require.NoError(h.t, err)
}

func (h *targetHandler) setExpectedHost(expectedHost string) {
	h.lock.Lock()
	h.expectedHost = expectedHost
	h.lock.Unlock()
}

func (h *targetHandler) setExpectedURLPath(expectedURLPath string) {
	h.lock.Lock()
	h.expectedURLPath = expectedURLPath
	h.lock.Unlock()
}
func (h *targetHandler) getExpectedURLPath() string {
	h.lock.Lock()
	defer h.lock.Unlock()
	return h.expectedURLPath
}

// func TestHTTPS(t *testing.T) {
// 	testApp := setUp(t, withCmdLineArgs("--router-port", strconv.Itoa(router.HTTPSPort), "--router-http01-challenge-host", "rtrtyry"))
// 	defer tearDown(testApp)

// 	resp := postURL(t, fmt.Sprintf("https://localhost:%d/unknown", router.HTTPSPort), nil)
// 	defer resp.Body.Close()

// 	respBody := readOK(t, resp)
// 	log.Println(string(respBody))
// }

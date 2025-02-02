/*
 * Copyright (c) 2022-present unTill Pro, Ltd.
 */

package state

import (
	"context"
	"io"
	"net/http"

	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/istructs"
)

type httpStorage struct{}

func (s *httpStorage) NewKeyBuilder(appdef.QName, istructs.IStateKeyBuilder) istructs.IStateKeyBuilder {
	return newHttpKeyBuilder()
}
func (s *httpStorage) Read(key istructs.IStateKeyBuilder, callback istructs.ValueCallback) (err error) {
	kb := key.(*httpKeyBuilder)

	ctx, cancel := context.WithTimeout(context.Background(), kb.timeout())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, kb.method(), kb.url(), kb.body())
	if err != nil {
		return err
	}

	for k, v := range kb.headers {
		req.Header.Add(k, v)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	bb, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	return callback(nil, &httpValue{
		body:       bb,
		header:     res.Header,
		statusCode: res.StatusCode,
	})
}

/*
 * Copyright (c) 2021-present unTill Pro, Ltd.
 */

package commandprocessor

import (
	"net/http"

	"github.com/voedger/voedger/pkg/appdef"
	coreutils "github.com/voedger/voedger/pkg/utils"
)

var (
	ViewQNamePLogKnownOffsets = appdef.NewQName(appdef.SysPackage, "PLogKnownOffsets")
	ViewQNameWLogKnownOffsets = appdef.NewQName(appdef.SysPackage, "WLogKnownOffsets")
	errWSNotInited            = coreutils.NewHTTPErrorf(http.StatusForbidden, "workspace is not initialized")
)

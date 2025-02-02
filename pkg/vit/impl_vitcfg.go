/*
 * Copyright (c) 2022-present unTill Pro, Ltd.
 */

package vit

import (
	"encoding/json"
	"fmt"

	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/apps"
	"github.com/voedger/voedger/pkg/apps/sys/blobberapp"
	"github.com/voedger/voedger/pkg/apps/sys/registryapp"
	"github.com/voedger/voedger/pkg/apps/sys/routerapp"
	"github.com/voedger/voedger/pkg/cluster"
	"github.com/voedger/voedger/pkg/extensionpoints"
	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/istructsmem"
	"github.com/voedger/voedger/pkg/sys/smtp"
	"github.com/voedger/voedger/pkg/sys/workspace"
	coreutils "github.com/voedger/voedger/pkg/utils"
	"github.com/voedger/voedger/pkg/vvm"
)

func NewOwnVITConfig(opts ...vitConfigOptFunc) VITConfig {
	// helper: implicitly append sys apps
	opts = append(opts,
		WithApp(istructs.AppQName_sys_registry, registryapp.Provide(smtp.Cfg{})),
		WithApp(istructs.AppQName_sys_blobber, blobberapp.Provide(smtp.Cfg{})),
		WithApp(istructs.AppQName_sys_router, routerapp.Provide(smtp.Cfg{})),
	)
	return VITConfig{opts: opts}
}

func NewSharedVITConfig(opts ...vitConfigOptFunc) VITConfig {
	cfg := NewOwnVITConfig(opts...)
	cfg.isShared = true
	return cfg
}

func WithBuilder(builder apps.AppBuilder) AppOptFunc {
	return func(app *app, cfg *vvm.VVMConfig) {
		cfg.VVMAppsBuilder.Add(app.name, builder)
	}
}

// at MainCluster
func WithUserLogin(name, pwd string, opts ...PostConstructFunc) AppOptFunc {
	return func(app *app, _ *vvm.VVMConfig) {
		login := NewLogin(name, pwd, app.name, istructs.SubjectKind_User, istructs.MainClusterID)
		for _, opt := range opts {
			opt(&login)
		}
		app.logins = append(app.logins, login)
	}
}

func WithWorkspaceTemplate(wsKind appdef.QName, templateName string, templateFS coreutils.EmbedFS) AppOptFunc {
	return func(app *app, cfg *vvm.VVMConfig) {
		app.wsTemplateFuncs = append(app.wsTemplateFuncs, func(ep extensionpoints.IExtensionPoint) {
			epWSKindTemplates := ep.ExtensionPoint(workspace.EPWSTemplates).ExtensionPoint(wsKind)
			epWSKindTemplates.AddNamed(templateName, templateFS)
		})
	}
}

func WithChild(wsKind appdef.QName, name, templateName string, templateParams string, ownerLoginName string, wsInitData map[string]interface{}, opts ...PostConstructFunc) PostConstructFunc {
	return func(intf interface{}) {
		wsParams := intf.(*WSParams)
		initData, err := json.Marshal(&wsInitData)
		if err != nil {
			panic(err)
		}
		newWSParams := WSParams{
			Name:           name,
			TemplateName:   templateName,
			TemplateParams: templateParams,
			Kind:           wsKind,
			ownerLoginName: ownerLoginName,
			InitDataJSON:   string(initData),
			ClusterID:      istructs.MainClusterID,
			docs:           map[appdef.QName]func(verifiedValues map[string]string) map[string]interface{}{},
		}
		for _, opt := range opts {
			opt(&newWSParams)
		}
		wsParams.childs = append(wsParams.childs, newWSParams)
	}
}

func WithChildWorkspace(wsKind appdef.QName, name, templateName string, templateParams string, ownerLoginName string, wsInitData map[string]interface{}, opts ...PostConstructFunc) AppOptFunc {
	return func(app *app, cfg *vvm.VVMConfig) {
		initData, err := json.Marshal(&wsInitData)
		if err != nil {
			panic(err)
		}
		wsParams := WSParams{
			Name:           name,
			TemplateName:   templateName,
			TemplateParams: templateParams,
			Kind:           wsKind,
			ownerLoginName: ownerLoginName,
			InitDataJSON:   string(initData),
			ClusterID:      istructs.MainClusterID,
			docs:           map[appdef.QName]func(verifiedValues map[string]string) map[string]interface{}{},
		}
		for _, opt := range opts {
			opt(&wsParams)
		}
		app.ws[name] = wsParams
	}
}

func WithDocWithVerifiedFields(name appdef.QName, dataFactory func(verifiedValues map[string]string) map[string]interface{}) PostConstructFunc {
	return func(intf interface{}) {
		switch t := intf.(type) {
		case *Login:
			t.docs[name] = dataFactory
		case *WSParams:
			t.docs[name] = dataFactory
		default:
			panic(fmt.Sprintln(t, name))
		}
	}
}

func WithDoc(name appdef.QName, data map[string]interface{}) PostConstructFunc {
	return WithDocWithVerifiedFields(name, func(verifiedValues map[string]string) map[string]interface{} {
		return data
	})
}

func WithSubject(login string, subjectKind istructs.SubjectKindType, roles []appdef.QName) PostConstructFunc {
	return func(intf interface{}) {
		wsParams := intf.(*WSParams)
		wsParams.subjects = append(wsParams.subjects, subject{
			login:       login,
			subjectKind: subjectKind,
			roles:       roles,
		})
	}
}

func WithVVMConfig(configurer func(cfg *vvm.VVMConfig)) vitConfigOptFunc {
	return func(hpc *vitPreConfig) {
		configurer(hpc.vvmCfg)
	}
}

func WithCleanup(cleanup func(*VIT)) vitConfigOptFunc {
	return func(hpc *vitPreConfig) {
		hpc.cleanups = append(hpc.cleanups, cleanup)
	}
}

func WithInit(initFunc func()) vitConfigOptFunc {
	return func(vpc *vitPreConfig) {
		vpc.initFuncs = append(vpc.initFuncs, initFunc)
	}
}

func WithApp(appQName istructs.AppQName, updater apps.AppBuilder, appOpts ...AppOptFunc) vitConfigOptFunc {
	return func(vpc *vitPreConfig) {
		_, ok := vpc.vitApps[appQName]
		if ok {
			panic("app already added")
		}
		app := &app{
			name: appQName,
			deployment: cluster.AppDeploymentDescriptor{
				PartsCount:     DefaultTestAppPartsCount,
				EnginePoolSize: DefaultTestAppEnginesPool,
			},
			ws:                    map[string]WSParams{},
			verifiedValuesIntents: map[string]verifiedValueIntent{},
		}
		vpc.vitApps[appQName] = app
		vpc.vvmCfg.VVMAppsBuilder.Add(appQName, updater)
		for _, appOpt := range appOpts {
			appOpt(app, vpc.vvmCfg)
		}
		// to append tests templates to already declared templates
		for _, wsTempalateFunc := range app.wsTemplateFuncs {
			vpc.vvmCfg.VVMAppsBuilder.Add(appQName, func(appAPI apps.APIs, cfg *istructsmem.AppConfigType, appDefBuilder appdef.IAppDefBuilder, ep extensionpoints.IExtensionPoint) (appPackages apps.AppPackages) {
				wsTempalateFunc(ep)
				return
			})
		}
	}
}

func WithVerifiedValue(docQName appdef.QName, fieldName string, desiredValue string) AppOptFunc {
	return func(app *app, cfg *vvm.VVMConfig) {
		app.verifiedValuesIntents[desiredValue] = verifiedValueIntent{
			docQName:     docQName,
			fieldName:    fieldName,
			desiredValue: desiredValue,
		}
	}
}

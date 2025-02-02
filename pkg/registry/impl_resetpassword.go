/*
 * Copyright (c) 2020-present unTill Pro, Ltd.
 */

package registry

import (
	"context"
	"fmt"
	"net/http"

	"github.com/voedger/voedger/pkg/istructs"
	"github.com/voedger/voedger/pkg/istructsmem"
	"github.com/voedger/voedger/pkg/itokens"
	payloads "github.com/voedger/voedger/pkg/itokens-payloads"
	"github.com/voedger/voedger/pkg/state"
	"github.com/voedger/voedger/pkg/sys/authnz"
	coreutils "github.com/voedger/voedger/pkg/utils"
)

func provideResetPassword(cfgRegistry *istructsmem.AppConfigType, asp istructs.IAppStructsProvider, itokens itokens.ITokens, federation coreutils.IFederation) {

	// sys/registry/pseudoProfileWSID/q.sys.InitiateResetPasswordByEmail
	// null auth
	cfgRegistry.Resources.Add(istructsmem.NewQueryFunction(
		QNameQueryInitiateResetPasswordByEmail,
		provideQryInitiateResetPasswordByEmailExec(asp, itokens, federation),
	))

	// sys/registry/pseudoProfileWSID/q.registry.IssueVerifiedValueTokenForResetPassword
	// null auth
	cfgRegistry.Resources.Add(istructsmem.NewQueryFunction(
		QNameQueryIssueVerifiedValueTokenForResetPassword,
		provideIssueVerifiedValueTokenForResetPasswordExec(itokens, federation),
	))

	cfgRegistry.Resources.Add(istructsmem.NewCommandFunction(
		QNameCommandResetPasswordByEmail,
		cmdResetPasswordByEmailExec,
	))
}

// sys/registry/pseudoWSID
// null auth
func provideQryInitiateResetPasswordByEmailExec(asp istructs.IAppStructsProvider, itokens itokens.ITokens, federation coreutils.IFederation) istructsmem.ExecQueryClosure {
	return func(ctx context.Context, args istructs.ExecQueryArgs, callback istructs.ExecQueryCallback) (err error) {
		loginAppStr := args.ArgumentObject.AsString(authnz.Field_AppName)
		email := args.ArgumentObject.AsString(field_Email)
		language := args.ArgumentObject.AsString(field_Language)
		login := email // TODO: considering login is email

		loginAppQName, err := istructs.ParseAppQName(loginAppStr)
		if err != nil {
			return coreutils.NewHTTPError(http.StatusBadRequest, err)
		}

		as, err := asp.AppStructs(loginAppQName)
		if err != nil {
			return err
		}

		// request is sent to pseudoProfileWSID, translated to AppWS
		if err = CheckAppWSID(login, args.Workspace, as.WSAmount()); err != nil {
			return err
		}

		cdocLoginID, err := GetCDocLoginID(args.State, args.Workspace, loginAppStr, login)
		if err != nil {
			return err
		}
		if cdocLoginID == 0 {
			return coreutils.NewHTTPErrorf(http.StatusBadRequest, "login does not exist")
		}

		// check CDoc<registry.Login>.WSID != 0
		kb, err := args.State.KeyBuilder(state.Record, QNameCDocLogin)
		if err != nil {
			return err
		}
		kb.PutRecordID(state.Field_ID, cdocLoginID)
		sv, err := args.State.MustExist(kb)
		if err != nil {
			return err
		}
		profileWSID := sv.AsInt64(authnz.Field_WSID)
		if profileWSID == 0 {
			return coreutils.NewHTTPErrorf(http.StatusLocked, "login profile is not initialized")
		}

		sysToken, err := payloads.GetSystemPrincipalToken(itokens, loginAppQName)
		if err != nil {
			return err
		}
		body := fmt.Sprintf(`{"args":{"Entity":"%s","Field":"%s","Email":"%s","TargetWSID":%d,"ForRegistry":true,"Language":"%s"},"elements":[{"fields":["VerificationToken"]}]}`,
			QNameCommandResetPasswordByEmailUnloggedParams, field_Email, email, profileWSID, language) // targetWSID - is the workspace we're going to use the verified value at
		resp, err := coreutils.FederationFunc(federation.URL(), fmt.Sprintf("api/%s/%d/q.sys.InitiateEmailVerification", loginAppQName, profileWSID), body, coreutils.WithAuthorizeBy(sysToken))
		if err != nil {
			return fmt.Errorf("q.sys.InitiateEmailVerification failed: %w", err)
		}

		verificationToken := resp.SectionRow()[0].(string)
		return callback(&result{token: verificationToken, profileWSID: profileWSID})
	}
}

// sys/registry/pseudoWSID
// null auth
func provideIssueVerifiedValueTokenForResetPasswordExec(itokens itokens.ITokens, federation coreutils.IFederation) istructsmem.ExecQueryClosure {
	return func(ctx context.Context, args istructs.ExecQueryArgs, callback istructs.ExecQueryCallback) (err error) {
		token := args.ArgumentObject.AsString(field_VerificationToken)
		code := args.ArgumentObject.AsString(field_VerificationCode)
		profileWSID := args.ArgumentObject.AsInt64(field_ProfileWSID)
		loginAppStr := args.ArgumentObject.AsString(authnz.Field_AppName)

		loginAppQName, err := istructs.ParseAppQName(loginAppStr)
		if err != nil {
			return coreutils.NewHTTPError(http.StatusBadRequest, err)
		}

		sysToken, err := payloads.GetSystemPrincipalToken(itokens, loginAppQName)
		if err != nil {
			return err
		}

		body := fmt.Sprintf(`{"args":{"VerificationToken":"%s","VerificationCode":"%s","ForRegistry":true},"elements":[{"fields":["VerifiedValueToken"]}]}`, token, code)
		resp, err := coreutils.FederationFunc(federation.URL(), fmt.Sprintf("api/%s/%d/q.sys.IssueVerifiedValueToken", loginAppQName, profileWSID), body, coreutils.WithAuthorizeBy(sysToken))
		if err != nil {
			return err
		}
		verificationToken := resp.SectionRow()[0].(string)
		return callback(&result{token: verificationToken})
	}
}

// sys/registry/pseudoWSID
// null auth
func cmdResetPasswordByEmailExec(args istructs.ExecCommandArgs) (err error) {
	email := args.ArgumentUnloggedObject.AsString(field_Email)
	newPwd := args.ArgumentUnloggedObject.AsString(field_NewPwd)
	appName := args.ArgumentObject.AsString(authnz.Field_AppName)
	login := email

	return ChangePassword(login, args.State, args.Intents, args.Workspace, appName, newPwd)
}

func (r *result) AsString(string) string {
	return r.token
}

func (r *result) AsInt64(string) int64 {
	return r.profileWSID
}

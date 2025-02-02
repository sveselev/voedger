/*
 * Copyright (c) 2023-present unTill Pro, Ltd.
 * @author Alisher Nurmanov
 */

package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/untillpro/goutils/logger"
	"golang.org/x/exp/maps"

	"github.com/voedger/voedger/cmd/vpm/internal/dm"
	"github.com/voedger/voedger/pkg/appdef"
	"github.com/voedger/voedger/pkg/parser"
	coreutils "github.com/voedger/voedger/pkg/utils"
)

func newCompileCmd() *cobra.Command {
	params := vpmParams{}
	cmd := &cobra.Command{
		Use:   "compile",
		Short: "compile voedger application",
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			params, err = prepareParams(params, args)
			if err != nil {
				return err
			}
			_, err = compile(params.WorkingDir)
			return
		},
	}
	initGlobalFlags(cmd, &params)
	return cmd
}

// compile compiles schemas in working dir and returns compile result
func compile(workingDir string) (*compileResult, error) {
	depMan, err := dm.NewGoBasedDependencyManager(workingDir)
	if err != nil {
		return nil, err
	}
	var errs []error

	var packages []*parser.PackageSchemaAST
	importedStmts := make(map[string]parser.ImportStmt)
	pkgFiles := make(packageFiles)
	// extract module path from dependency file
	goModFileDir := filepath.Dir(depMan.DependencyFilePath())
	relativeModulePath, err := filepath.Rel(goModFileDir, workingDir)
	if err != nil {
		return nil, err
	}
	relativeModulePath = strings.ReplaceAll(relativeModulePath, "\\", "/")
	modulePath, err := url.JoinPath(depMan.ModulePath(), relativeModulePath)
	if err != nil {
		return nil, err
	}

	// compile sys package first
	sysPackageAst, compileSysErrs := compileSys(depMan, importedStmts, pkgFiles)
	packages = append(packages, sysPackageAst...)
	errs = append(errs, compileSysErrs...)

	// compile working dir after sys package
	compileDirPackageAst, compileDirErrs := compileDir(depMan, workingDir, modulePath, nil, importedStmts, pkgFiles)
	packages = append(packages, compileDirPackageAst...)
	errs = append(errs, compileDirErrs...)

	// add dummy app schema if no app schema found
	if !hasAppSchema(packages) {
		appPackageAst, err := getDummyAppPackageAst(maps.Values(importedStmts))
		if err != nil {
			errs = append(errs, err)
		}
		packages = append(packages, appPackageAst)
		addMissingUses(appPackageAst, getUseStmts(maps.Values(importedStmts)))
	}

	// remove nil packages
	nonNilPackages := make([]*parser.PackageSchemaAST, 0, len(packages))
	for _, p := range packages {
		if p != nil {
			nonNilPackages = append(nonNilPackages, p)
		}
	}
	// build app schema
	appAst, err := parser.BuildAppSchema(nonNilPackages)
	if err != nil {
		errs = append(errs, coreutils.SplitErrors(err)...)
	}
	// build app defs from app schema
	if appAst != nil {
		if err := parser.BuildAppDefs(appAst, appdef.New()); err != nil {
			errs = append(errs, coreutils.SplitErrors(err)...)
		}
	}
	if len(errs) == 0 {
		if logger.IsVerbose() {
			logger.Verbose("compiling succeeded")
		}
	}
	return &compileResult{
		modulePath:   modulePath,
		pkgFiles:     pkgFiles,
		appSchemaAST: appAst,
	}, errors.Join(errs...)
}

func hasAppSchema(packages []*parser.PackageSchemaAST) bool {
	for _, p := range packages {
		if p != nil {
			for _, f := range p.Ast.Statements {
				if f.Application != nil {
					return true
				}
			}
		}
	}
	return false
}

func getDummyAppPackageAst(imports []parser.ImportStmt) (*parser.PackageSchemaAST, error) {
	fileAst := &parser.FileSchemaAST{
		FileName: sysSchemaSqlFileName,
		Ast: &parser.SchemaAST{
			Imports: imports,
			Statements: []parser.RootStatement{
				{
					Application: &parser.ApplicationStmt{
						Name: dummyAppName,
					},
				},
			},
		},
	}
	return parser.BuildPackageSchema(dummyAppName, []*parser.FileSchemaAST{fileAst})
}

func getUseStmts(imports []parser.ImportStmt) []parser.UseStmt {
	uses := make([]parser.UseStmt, len(imports))
	for i, imp := range imports {
		use := parser.Ident(filepath.Base(imp.Name))
		if imp.Alias != nil {
			use = *imp.Alias
		}
		uses[i] = parser.UseStmt{
			Name: use,
		}
	}
	return uses
}

func addMissingUses(appPackage *parser.PackageSchemaAST, uses []parser.UseStmt) {
	for _, f := range appPackage.Ast.Statements {
		if f.Application != nil {
			for _, use := range uses {
				found := false
				for _, useInApp := range f.Application.Uses {
					if useInApp.Name == use.Name {
						found = true
						break
					}
				}
				if !found {
					f.Application.Uses = append(f.Application.Uses, use)
				}
			}
		}
	}
}

func compileSys(depMan dm.IDependencyManager, importedStmts map[string]parser.ImportStmt, pkgFiles packageFiles) ([]*parser.PackageSchemaAST, []error) {
	return compileDependency(depMan, appdef.SysPackage, nil, importedStmts, pkgFiles)
}

// checkImportedStmts checks if qpn is already imported. If not, it adds it to importedStmts
func checkImportedStmts(qpn string, alias *parser.Ident, importedStmts map[string]parser.ImportStmt) bool {
	aliasPtr := alias
	// workaround for sys package
	if qpn == appdef.SysPackage || qpn == sysQPN {
		qpn = appdef.SysPackage
		alias := parser.Ident(qpn)
		aliasPtr = &alias
	}
	if _, exists := importedStmts[qpn]; exists {
		return false
	}
	importedStmts[qpn] = parser.ImportStmt{
		Name:  qpn,
		Alias: aliasPtr,
	}
	return true
}

func compileDir(depMan dm.IDependencyManager, dir, qpn string, alias *parser.Ident, importedStmts map[string]parser.ImportStmt, pkgFiles packageFiles) (packages []*parser.PackageSchemaAST, errs []error) {
	if ok := checkImportedStmts(qpn, alias, importedStmts); !ok {
		return
	}
	if logger.IsVerbose() {
		logger.Verbose(fmt.Sprintf("compiling %s", dir))
	}

	packageAst, fileNames, err := parser.ParsePackageDirCollectingFiles(qpn, coreutils.NewPathReader(dir), "")
	if err != nil {
		errs = append(errs, coreutils.SplitErrors(err)...)
	}
	// collect all the files that belong to the package
	for _, f := range fileNames {
		pkgFiles[qpn] = append(pkgFiles[qpn], filepath.Join(dir, f))
	}
	// iterate over all imports and compile them as well
	var compileDepErrs []error
	var importedPackages []*parser.PackageSchemaAST
	if packageAst != nil {
		importedPackages, compileDepErrs = compileDependencies(depMan, packageAst.Ast.Imports, importedStmts, pkgFiles)
		errs = append(errs, compileDepErrs...)
	}
	packages = append([]*parser.PackageSchemaAST{packageAst}, importedPackages...)
	return
}

func compileDependencies(depMan dm.IDependencyManager, imports []parser.ImportStmt, importedStmts map[string]parser.ImportStmt, pkgFiles packageFiles) (packages []*parser.PackageSchemaAST, errs []error) {
	for _, imp := range imports {
		dependentPackages, compileDepErrs := compileDependency(depMan, imp.Name, imp.Alias, importedStmts, pkgFiles)
		errs = append(errs, compileDepErrs...)
		packages = append(packages, dependentPackages...)
	}
	return
}

func compileDependency(depMan dm.IDependencyManager, depURL string, alias *parser.Ident, importedStmts map[string]parser.ImportStmt, pkgFiles packageFiles) (packages []*parser.PackageSchemaAST, errs []error) {
	// workaround for sys package
	depURLToFind := depURL
	if depURL == appdef.SysPackage {
		depURLToFind = sysQPN
	}
	localPath, err := depMan.LocalPath(depURLToFind)
	if err != nil {
		errs = append(errs, err)
	}
	if logger.IsVerbose() {
		logger.Verbose(fmt.Sprintf("dependency: %s\nlocation: %s\n", depURL, localPath))
	}
	var compileDirErrs []error
	packages, compileDirErrs = compileDir(depMan, localPath, depURL, alias, importedStmts, pkgFiles)
	errs = append(errs, compileDirErrs...)
	return
}

func makeAbsPath(dir string) (string, error) {
	if !filepath.IsAbs(dir) {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get working directory: %v", err)
		}
		dir = filepath.Clean(filepath.Join(wd, dir))
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "", fmt.Errorf("failed to open %s", dir)
	}
	return dir, nil
}

func initGlobalFlags(cmd *cobra.Command, params *vpmParams) {
	cmd.SilenceErrors = true
	cmd.Flags().StringVarP(&params.WorkingDir, "change-dir", "C", "", "Change to dir before running the command. Any files named on the command line are interpreted after changing directories. If used, this flag must be the first one in the command line.")
}

func prepareParams(params vpmParams, args []string) (newParams vpmParams, err error) {
	if len(args) > 0 {
		params.TargetDir = filepath.Clean(args[0])
	}
	newParams = params
	newParams.WorkingDir, err = makeAbsPath(params.WorkingDir)
	if err != nil {
		return
	}
	if newParams.IgnoreFile != "" {
		newParams.IgnoreFile = filepath.Clean(newParams.IgnoreFile)
	}
	if newParams.TargetDir == "" {
		newParams.TargetDir = newParams.WorkingDir
	}
	return
}

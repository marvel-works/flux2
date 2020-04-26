package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the toolkit components",
	Long: `
The install command deploys the toolkit components
on the configured Kubernetes cluster in ~/.kube/config`,
	Example: `  install --version=master --namespace=gitops-systems`,
	RunE:    installCmdRun,
}

var (
	installDryRun        bool
	installManifestsPath string
	installVersion       string
)

func init() {
	installCmd.Flags().BoolVarP(&installDryRun, "dry-run", "", false,
		"only print the object that would be applied")
	installCmd.Flags().StringVarP(&installVersion, "version", "v", "master",
		"toolkit tag or branch")
	installCmd.Flags().StringVarP(&installManifestsPath, "manifests", "", "",
		"path to the manifest directory, dev only")
	rootCmd.AddCommand(installCmd)
}

func installCmdRun(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var kustomizePath string
	if installVersion == "" && !strings.HasPrefix(installManifestsPath, "github.com/") {
		if _, err := os.Stat(installManifestsPath); err != nil {
			return fmt.Errorf("manifests not found: %w", err)
		}
		kustomizePath = installManifestsPath
	}

	tmpDir, err := ioutil.TempDir("", namespace)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	logAction("generating install manifests")
	if kustomizePath == "" {
		err = genInstallManifests(installVersion, namespace, tmpDir)
		if err != nil {
			return fmt.Errorf("install failed: %w", err)
		}
		kustomizePath = tmpDir
	}

	manifest := path.Join(tmpDir, fmt.Sprintf("%s.yaml", namespace))
	command := fmt.Sprintf("kustomize build %s > %s", kustomizePath, manifest)
	if _, err := utils.execCommand(ctx, ModeStderrOS, command); err != nil {
		return fmt.Errorf("install failed")
	}

	command = fmt.Sprintf("cat %s", manifest)
	if yaml, err := utils.execCommand(ctx, ModeCapture, command); err != nil {
		return fmt.Errorf("install failed: %w", err)
	} else {
		if verbose {
			fmt.Print(yaml)
		}
	}
	logSuccess("build completed")

	logAction("installing components in %s namespace", namespace)
	applyOutput := ModeStderrOS
	if verbose {
		applyOutput = ModeOS
	}
	dryRun := ""
	if installDryRun {
		dryRun = "--dry-run=client"
		applyOutput = ModeOS
	}
	command = fmt.Sprintf("cat %s | kubectl apply -f- %s", manifest, dryRun)
	if _, err := utils.execCommand(ctx, applyOutput, command); err != nil {
		return fmt.Errorf("install failed")
	}

	if installDryRun {
		logSuccess("install dry-run finished")
		return nil
	} else {
		logSuccess("install completed")
	}

	logAction("verifying installation")
	for _, deployment := range []string{"source-controller", "kustomize-controller"} {
		command = fmt.Sprintf("kubectl -n %s rollout status deployment %s --timeout=%s",
			namespace, deployment, timeout.String())
		if _, err := utils.execCommand(ctx, applyOutput, command); err != nil {
			return fmt.Errorf("install failed")
		} else {
			logSuccess("%s ready", deployment)
		}
	}

	logSuccess("install finished")
	return nil
}

var namespaceTmpl = `---
apiVersion: v1
kind: Namespace
metadata:
  name: {{.Namespace}}
`

var labelsTmpl = `---
apiVersion: builtin
kind: LabelTransformer
metadata:
  name: labels
labels:
  app.kubernetes.io/instance: {{.Namespace}}
  app.kubernetes.io/version: "{{.Version}}"
fieldSpecs:
  - path: metadata/labels
    create: true
`

var kustomizationTmpl = `---
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: {{.Namespace}}
transformers:
  - labels.yaml
resources:
  - namespace.yaml
  - roles
  - github.com/fluxcd/toolkit/manifests/bases/source-controller?ref={{.Version}}
  - github.com/fluxcd/toolkit/manifests/bases/kustomize-controller?ref={{.Version}}
  - github.com/fluxcd/toolkit/manifests/policies?ref={{.Version}}
`

var kustomizationRolesTmpl = `---
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - github.com/fluxcd/toolkit/manifests/rbac?ref={{.Version}}
nameSuffix: -{{.Namespace}}
`

func genInstallManifests(ver, ns, tmpDir string) error {
	model := struct {
		Version   string
		Namespace string
	}{
		Version:   ver,
		Namespace: ns,
	}

	if err := utils.execTemplate(model, namespaceTmpl, path.Join(tmpDir, "namespace.yaml")); err != nil {
		return fmt.Errorf("generate namespace failed: %w", err)
	}

	if err := utils.execTemplate(model, labelsTmpl, path.Join(tmpDir, "labels.yaml")); err != nil {
		return fmt.Errorf("generate labels failed: %w", err)
	}

	if err := utils.execTemplate(model, kustomizationTmpl, path.Join(tmpDir, "kustomization.yaml")); err != nil {
		return fmt.Errorf("generate kustomization failed: %w", err)
	}

	if err := os.MkdirAll(path.Join(tmpDir, "roles"), os.ModePerm); err != nil {
		return fmt.Errorf("generate roles failed: %w", err)
	}

	if err := utils.execTemplate(model, kustomizationRolesTmpl, path.Join(tmpDir, "roles/kustomization.yaml")); err != nil {
		return fmt.Errorf("generate roles failed: %w", err)
	}

	return nil
}

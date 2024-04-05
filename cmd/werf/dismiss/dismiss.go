package dismiss

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	helm_v3 "helm.sh/helm/v3/cmd/helm"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/registry"
	helmrelease "helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"

	"github.com/werf/kubedog/pkg/kube"
	"github.com/werf/logboek"
	"github.com/werf/werf/cmd/werf/common"
	"github.com/werf/werf/pkg/config/deploy_params"
	"github.com/werf/werf/pkg/deploy/helm"
	"github.com/werf/werf/pkg/deploy/helm/command_helpers"
	"github.com/werf/werf/pkg/deploy/lock_manager"
	"github.com/werf/werf/pkg/git_repo"
	"github.com/werf/werf/pkg/git_repo/gitdata"
	"github.com/werf/werf/pkg/giterminism_manager"
	"github.com/werf/werf/pkg/image"
	"github.com/werf/werf/pkg/storage/lrumeta"
	"github.com/werf/werf/pkg/true_git"
	"github.com/werf/werf/pkg/util"
	"github.com/werf/werf/pkg/werf"
	"github.com/werf/werf/pkg/werf/global_warnings"
)

var cmdData struct {
	WithNamespace bool
	WithHooks     bool
}

var commonCmdData common.CmdData

func NewCmd(ctx context.Context) *cobra.Command {
	ctx = common.NewContextWithCmdData(ctx, &commonCmdData)
	cmd := common.SetCommandContext(ctx, &cobra.Command{
		Use:   "dismiss",
		Short: "Delete werf release from Kubernetes",
		Long:  common.GetLongCommandDescription(GetDismissDocs().Long),
		Example: `  # Dismiss werf release with release name and namespace autogenerated from werf.yaml configuration (Git required):
  $ werf dismiss --env dev

  # Dismiss werf release with explicitly specified release name and namespace (no Git required):
  $ werf dismiss --namespace mynamespace --release myrelease-dev

  # Save the deploy report with the "converge" command and use namespace and release name, saved in this deploy report, in the "dismiss" command (no Git required for dismiss):
  $ werf converge --save-deploy-report --env dev
  $ cp .werf-deploy-report.json /anywhere/
  $ cd /anywhere
  $ werf dismiss --use-deploy-report  # Git not needed anymore, only the deploy report file.

  # Dismiss with namespace:
  $ werf dismiss --env dev --with-namespace`,
		DisableFlagsInUseLine: true,
		Annotations: map[string]string{
			common.DocsLongMD: GetDismissDocs().LongMD,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			defer global_warnings.PrintGlobalWarnings(ctx)

			if err := common.ProcessLogOptions(&commonCmdData); err != nil {
				common.PrintHelp(cmd)
				return err
			}
			common.LogVersion()

			return common.LogRunningTime(func() error {
				return runDismiss(ctx)
			})
		},
	})

	common.SetupTmpDir(&commonCmdData, cmd, common.SetupTmpDirOptions{})
	common.SetupConfigTemplatesDir(&commonCmdData, cmd)
	common.SetupConfigPath(&commonCmdData, cmd)
	common.SetupGiterminismConfigPath(&commonCmdData, cmd)
	common.SetupEnvironment(&commonCmdData, cmd)

	common.SetupGiterminismOptions(&commonCmdData, cmd)

	common.SetupHomeDir(&commonCmdData, cmd, common.SetupHomeDirOptions{})
	common.SetupDir(&commonCmdData, cmd)
	common.SetupGitWorkTree(&commonCmdData, cmd)

	common.SetupSecondaryStagesStorageOptions(&commonCmdData, cmd)
	common.SetupCacheStagesStorageOptions(&commonCmdData, cmd)
	common.SetupRepoOptions(&commonCmdData, cmd, common.RepoDataOptions{})
	common.SetupFinalRepo(&commonCmdData, cmd)
	common.SetupSynchronization(&commonCmdData, cmd)

	common.SetupRelease(&commonCmdData, cmd, true)
	common.SetupNamespace(&commonCmdData, cmd, true)

	common.SetupUseDeployReport(&commonCmdData, cmd)
	common.SetupDeployReportPath(&commonCmdData, cmd)

	common.SetupKubeConfig(&commonCmdData, cmd)
	common.SetupKubeConfigBase64(&commonCmdData, cmd)
	common.SetupKubeContext(&commonCmdData, cmd)

	common.SetupStatusProgressPeriod(&commonCmdData, cmd)
	common.SetupHooksStatusProgressPeriod(&commonCmdData, cmd)
	common.SetupReleasesHistoryMax(&commonCmdData, cmd)

	common.SetupDockerConfig(&commonCmdData, cmd, "")

	common.SetupLogOptions(&commonCmdData, cmd)
	common.SetupLogProjectDir(&commonCmdData, cmd)

	common.SetupDisableAutoHostCleanup(&commonCmdData, cmd)
	common.SetupAllowedDockerStorageVolumeUsage(&commonCmdData, cmd)
	common.SetupAllowedDockerStorageVolumeUsageMargin(&commonCmdData, cmd)
	common.SetupAllowedLocalCacheVolumeUsage(&commonCmdData, cmd)
	common.SetupAllowedLocalCacheVolumeUsageMargin(&commonCmdData, cmd)
	common.SetupDockerServerStoragePath(&commonCmdData, cmd)

	common.SetupInsecureRegistry(&commonCmdData, cmd)
	common.SetupInsecureHelmDependencies(&commonCmdData, cmd, true)
	common.SetupSkipTlsVerifyRegistry(&commonCmdData, cmd)

	commonCmdData.SetupPlatform(cmd)

	cmd.Flags().BoolVarP(&cmdData.WithNamespace, "with-namespace", "", util.GetBoolEnvironmentDefaultFalse("WERF_WITH_NAMESPACE"), "Delete Kubernetes Namespace after purging Helm Release (default $WERF_WITH_NAMESPACE)")
	cmd.Flags().BoolVarP(&cmdData.WithHooks, "with-hooks", "", util.GetBoolEnvironmentDefaultTrue("WERF_WITH_HOOKS"), "Delete Helm Release hooks getting from existing revisions (default $WERF_WITH_HOOKS or true)")

	return cmd
}

func runDismiss(ctx context.Context) error {
	if err := werf.Init(*commonCmdData.TmpDir, *commonCmdData.HomeDir); err != nil {
		return fmt.Errorf("initialization error: %w", err)
	}

	containerBackend, processCtx, err := common.InitProcessContainerBackend(ctx, &commonCmdData)
	if err != nil {
		return err
	}
	ctx = processCtx

	_ = containerBackend

	gitDataManager, err := gitdata.GetHostGitDataManager(ctx)
	if err != nil {
		return fmt.Errorf("error getting host git data manager: %w", err)
	}

	if err := git_repo.Init(gitDataManager); err != nil {
		return err
	}

	if err := true_git.Init(ctx, true_git.Options{LiveGitOutput: *commonCmdData.LogDebug}); err != nil {
		return err
	}

	if err := image.Init(); err != nil {
		return err
	}

	if err := lrumeta.Init(); err != nil {
		return err
	}

	common.LogKubeContext(kube.Context)

	giterminismManager, err := common.GetGiterminismManager(ctx, &commonCmdData)
	var gitNotFoundErr *common.GitWorktreeNotFoundError
	if err != nil {
		if !errors.As(err, &gitNotFoundErr) {
			return err
		}
	}
	gitFound := gitNotFoundErr == nil

	common.SetupOndemandKubeInitializer(*commonCmdData.KubeContext, *commonCmdData.KubeConfig, *commonCmdData.KubeConfigBase64, *commonCmdData.KubeConfigPathMergeList)
	if err := common.GetOndemandKubeInitializer().Init(ctx); err != nil {
		return err
	}

	namespace, release, err := getNamespaceAndRelease(ctx, gitFound, giterminismManager)
	if err != nil {
		return err
	}

	var helmRegistryClient *registry.Client
	if gitFound {
		helmRegistryClient, err = common.NewHelmRegistryClient(ctx, *commonCmdData.DockerConfig, *commonCmdData.InsecureHelmDependencies)
		if err != nil {
			return fmt.Errorf("unable to create helm registry client: %w", err)
		}
	}

	var lockManager *lock_manager.LockManager
	if !cmdData.WithNamespace {
		if m, err := lock_manager.NewLockManager(namespace); err != nil {
			return fmt.Errorf("unable to create lock manager: %w", err)
		} else {
			lockManager = m
		}
	}

	actionConfig := new(action.Configuration)
	if err := helm.InitActionConfig(ctx, common.GetOndemandKubeInitializer(), namespace, helm_v3.Settings, actionConfig, helm.InitActionConfigOptions{
		StatusProgressPeriod:      time.Duration(*commonCmdData.StatusProgressPeriodSeconds) * time.Second,
		HooksStatusProgressPeriod: time.Duration(*commonCmdData.HooksStatusProgressPeriodSeconds) * time.Second,
		KubeConfigOptions: kube.KubeConfigOptions{
			Context:          *commonCmdData.KubeContext,
			ConfigPath:       *commonCmdData.KubeConfig,
			ConfigDataBase64: *commonCmdData.KubeConfigBase64,
		},
		ReleasesHistoryMax: *commonCmdData.ReleasesHistoryMax,
		RegistryClient:     helmRegistryClient,
	}); err != nil {
		return err
	}

	dontFailIfNoRelease := true
	helmUninstallCmd := helm_v3.NewUninstallCmd(actionConfig, logboek.Context(ctx).OutStream(), helm_v3.UninstallCmdOptions{
		StagesSplitter:      helm.NewStagesSplitter(),
		DeleteNamespace:     &cmdData.WithNamespace,
		DeleteHooks:         &cmdData.WithHooks,
		DontFailIfNoRelease: &dontFailIfNoRelease,
	})

	logboek.Context(ctx).Default().LogFDetails("Using namespace: %s\n", namespace)
	logboek.Context(ctx).Default().LogFDetails("Using release: %s\n", release)

	if cmdData.WithNamespace {
		// TODO: solve lock release + delete-namespace case
		return helmUninstallCmd.RunE(helmUninstallCmd, []string{release})
	} else {
		if _, err := actionConfig.Releases.History(release); errors.Is(err, driver.ErrReleaseNotFound) {
			logboek.Context(ctx).Default().LogFDetails("No such release %q\n", release)
			return nil
		}

		return command_helpers.LockReleaseWrapper(ctx, release, lockManager, func() error {
			return helmUninstallCmd.RunE(helmUninstallCmd, []string{release})
		})
	}
}

func getNamespaceAndRelease(ctx context.Context, gitFound bool, giterminismMgr giterminism_manager.Interface) (string, string, error) {
	namespaceSpecified := *commonCmdData.Namespace != ""
	releaseSpecified := *commonCmdData.Release != ""

	var namespace string
	var release string
	if common.GetUseDeployReport(&commonCmdData) {
		if namespaceSpecified || releaseSpecified {
			return "", "", fmt.Errorf("--namespace or --release can't be used together with --use-deploy-report")
		}

		deployReportPath, err := common.GetDeployReportPath(&commonCmdData)
		if err != nil {
			return "", "", fmt.Errorf("unable to get deploy report path: %w", err)
		}

		deployReportByte, err := os.ReadFile(deployReportPath)
		if err != nil {
			return "", "", fmt.Errorf("unable to read deploy report file %q: %w", deployReportPath, err)
		}

		var deployReport helmrelease.DeployReport
		if err := json.Unmarshal(deployReportByte, &deployReport); err != nil {
			return "", "", fmt.Errorf("unable to unmarshal deploy report file %q: %w", deployReportPath, err)
		}

		if deployReport.Namespace == "" {
			return "", "", fmt.Errorf("unable to get namespace from deploy report file %q", deployReportPath)
		}

		if deployReport.Release == "" {
			return "", "", fmt.Errorf("unable to get release from deploy report file %q", deployReportPath)
		}

		namespace = deployReport.Namespace
		release = deployReport.Release
	} else if gitFound {
		common.ProcessLogProjectDir(&commonCmdData, giterminismMgr.ProjectDir())

		_, werfConfig, err := common.GetRequiredWerfConfig(ctx, &commonCmdData, giterminismMgr, common.GetWerfConfigOptions(&commonCmdData, true))
		if err != nil {
			return "", "", fmt.Errorf("unable to load werf config: %w", err)
		}
		logboek.LogOptionalLn()

		namespace, err = deploy_params.GetKubernetesNamespace(*commonCmdData.Namespace, *commonCmdData.Environment, werfConfig)
		if err != nil {
			return "", "", err
		}

		release, err = deploy_params.GetHelmRelease(*commonCmdData.Release, *commonCmdData.Environment, namespace, werfConfig)
		if err != nil {
			return "", "", err
		}
	} else if !gitFound {
		if !namespaceSpecified && !releaseSpecified {
			return "", "", fmt.Errorf("no git with werf project found: dismiss should either be executed in a git repository, or with --namespace and --release specified, or with --use-deploy-report")
		} else if namespaceSpecified && !releaseSpecified {
			return "", "", fmt.Errorf("--namespace specified, but not --release, while should be specified both or none")
		} else if !namespaceSpecified && releaseSpecified {
			return "", "", fmt.Errorf("--release specified, but not --namespace, while should be specified both or none")
		}

		namespace = *commonCmdData.Namespace
		release = *commonCmdData.Release
	}

	return namespace, release, nil
}

package cfstack

import (
	"fmt"
	"git.wizrocket.net/infra/cfstack/internal/pkg/aws/cloudformation"
	"git.wizrocket.net/infra/cfstack/internal/pkg/aws/s3"
	"git.wizrocket.net/infra/cfstack/internal/pkg/aws/session"
	"git.wizrocket.net/infra/cfstack/internal/pkg/manifest"
	"git.wizrocket.net/infra/cfstack/internal/pkg/util"
	"git.wizrocket.net/infra/cfstack/internal/pkg/worker"
	"github.com/Jeffail/gabs"
	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type DeployOpts struct {
	manifestFile string
	valuesFile   string
	profile      string
	role         string

	workers int

	deployStackOpts *DeployStackOpts

	uid           string
	templatesRoot string

	manifest manifest.Manifest
	values   *gabs.Container
}

func (opts *DeployOpts) preRun() error {
	templatesRoot, err := filepath.Abs(filepath.Dir(opts.manifestFile))
	if err != nil {
		return err
	}
	opts.templatesRoot = templatesRoot

	err = opts.manifest.Parse(opts.manifestFile)
	if err != nil {
		return err
	}

	if len(opts.valuesFile) > 0 {
		if !filepath.IsAbs(opts.valuesFile) {
			opts.valuesFile = filepath.Join(templatesRoot, opts.valuesFile)
		}
		opts.values, err = util.ParseJsonFile(opts.valuesFile)
		if err != nil {
			return err
		}
	}

	if !opts.manifest.ParallelDeployment {
		opts.workers = 1
	}

	uid, err := uuid.NewUUID()
	if err != nil {
		return err
	}
	opts.uid = uid.String()

	return nil
}

func (opts *DeployOpts) Run() error {

	regionJobs := make(chan worker.RegionDeployWorkerJob, len(opts.manifest.Regions))
	results := make(chan *worker.RegionDeployWorkerResult, len(opts.manifest.Regions))

	// start workers
	wg := sync.WaitGroup{}

	workers := 1

	if opts.manifest.ParallelDeployment {
		workers = len(opts.manifest.Regions)
	}

	for i := 1; i <= workers; i++ {
		go worker.RegionDeployWorker(i, &wg, regionJobs, results)
	}

	for _, region := range opts.manifest.Regions {
		regionJobs <- worker.RegionDeployWorkerJob{
			Region:             region.Name,
			Stacks:             region.Stacks,
			Profile:            opts.profile,
			StackDeployWorkers: opts.workers,
			Uid:                opts.uid,
			TemplatesRoot:      opts.templatesRoot,
			Values:             opts.values,
			Role:               opts.role,
			ParallelMode:       opts.manifest.ParallelDeployment,
		}
		wg.Add(1)
	}
	close(regionJobs)
	wg.Wait()

	var errRegions []string

	for i := 1; i <= len(opts.manifest.Regions); i++ {
		select {
		case result := <-results:
			if result.Err != nil {
				color.New(color.FgRed).Fprintf(os.Stdout, "    %v\n", result.Err)
				errRegions = append(errRegions, result.Region)
			}
		}
	}

	if len(errRegions) > 0 {
		return errors.Errorf("Deployment failed in region(s): %s", strings.Join(errRegions, ", "))
	}
	return nil
}

func (opts *DeployOpts) RunSerial() error {
	for _, region := range opts.manifest.Regions {
		stacks := region.Stacks
		sess, err := session.NewSession(&session.Opts{
			Profile: opts.profile,
			Region:  region.Name,
		})

		if err != nil {
			return err
		}

		uploader := s3.New(sess)
		deployer := cloudformation.New(sess, opts.values)

		bucket, err := deployer.GetStackResourcePhysicalId("cfstack-Init", "TemplatesS3Bucket")

		if err != nil {
			return err
		}

		for i, s := range stacks {
			fmt.Printf("==> %s  Deploying stack %s in region %s\n", rocket, s.StackName, region.Name)
			s.SetRegion(region.Name)
			s.SetUuid(opts.uid)
			s.SetBucket(bucket)
			s.SetDeploymentOrder(i)
			s.TemplateRootPath = opts.templatesRoot

			s.Uploader = uploader
			s.Deployer = deployer

			s.RoleArn = opts.role

			err = s.Deploy()

			if err != nil {
				fmt.Fprintf(os.Stdout, color.RedString("    %v\n", err))
				return fmt.Errorf("%s stack deployment has failed", s.StackName)
			}
		}
	}
	return nil
}

func NewDeployCmd() *cobra.Command {
	opts := &DeployOpts{}
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy your cloudformation stacks",
		Long:  `Deploys cloudformation stacks defined in manifest files.`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return opts.preRun()
		},
		Run: func(cmd *cobra.Command, args []string) {
			err := opts.Run()
			if err != nil {
				ExitWithError("Deploy", err)
			}
			color.New(color.Bold, color.FgGreen).Fprintf(os.Stdout, "\nDeploy stacks command completed\n")
		},
	}

	cmd.PersistentFlags().StringVarP(&opts.manifestFile, "manifest", "m", "", "Set your manifest file")
	cmd.PersistentFlags().StringVarP(&opts.valuesFile, "values", "", "values.json", "Set your values file")
	cmd.PersistentFlags().StringVarP(&opts.profile, "profile", "", "default", "Profile to use from AWS credentials")
	cmd.PersistentFlags().StringVarP(&opts.role, "role", "", "", "Cloudformation service role to be used for stack operations")
	cmd.Flags().IntVarP(&opts.workers, "workers", "w", worker.MaxWorker, "No of concurrent workers for deploying stacks")
	cmd.AddCommand(opts.NewDeployStackCmd())

	return cmd
}

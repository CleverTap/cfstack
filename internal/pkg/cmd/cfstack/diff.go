package cfstack

import (
	"encoding/json"
	"fmt"
	"git.wizrocket.net/infra/cfstack/internal/pkg/manifest"
	"git.wizrocket.net/infra/cfstack/internal/pkg/util"
	"git.wizrocket.net/infra/cfstack/internal/pkg/worker"
	"github.com/Jeffail/gabs"
	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"sync"
)

type DiffOpts struct {
	manifestFile string
	valuesFile   string
	workers      int
	profile      string
	role         string

	uid           string
	templatesRoot string

	manifest manifest.Manifest
	values   *gabs.Container
}

func (opts *DiffOpts) Run() error {
	regionJobs := make(chan worker.RegionDiffWorkerJob, len(opts.manifest.Regions))
	results := make(chan *worker.RegionDiffWorkerResult, len(opts.manifest.Regions))

	// start workers
	wg := sync.WaitGroup{}

	workers := len(opts.manifest.Regions)

	for i := 1; i <= workers; i++ {
		go worker.RegionDiffWorker(i, &wg, regionJobs, results)
	}

	for _, region := range opts.manifest.Regions {
		regionJobs <- worker.RegionDiffWorkerJob{
			Region:          region.Name,
			Stacks:          region.Stacks,
			Profile:         opts.profile,
			StackDiffWorker: opts.workers,
			Uid:             opts.uid,
			TemplatesRoot:   opts.templatesRoot,
			Values:          opts.values,
			Role:            opts.role,
		}
		wg.Add(1)
	}
	close(regionJobs)
	wg.Wait()

	out := make([]manifest.Region, 0)

	var errResult error

	for i := 1; i <= workers; i++ {
		select {
		case result := <-results:
			if len(result.Stacks) > 0 {
				region := manifest.Region{
					Name:   result.Region,
					Stacks: result.Stacks,
				}
				out = append(out, region)
			}
			if result.Err != nil {
				color.New(color.FgRed).Fprintf(os.Stdout, "    %v\n", result.Err)
				errResult = fmt.Errorf("diff for %s has failed with a few errors", result.Region)
			}
		}
	}

	res := manifest.Manifest{Regions: out, ParallelDeployment: opts.manifest.ParallelDeployment}

	s, err := json.MarshalIndent(&res, "", "  ")
	if err != nil {
		return err
	}

	err = util.WriteToFile(s, "diff.json")

	if err != nil {
		return err
	}

	return errResult
}

func NewDiffCmd() *cobra.Command {
	opts := &DiffOpts{}
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Diff for you CloudFormation changes",
		Long:  `Generates a diff of the changes to CloudFormation stacks defined in manifest files.`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			templatesRoot, err := filepath.Abs(filepath.Dir(opts.manifestFile))
			if err != nil {
				return err
			}
			opts.templatesRoot = templatesRoot

			err = opts.manifest.Parse(opts.manifestFile)
			if err != nil {
				return err
			}

			if !filepath.IsAbs(opts.valuesFile) {
				opts.valuesFile = filepath.Join(templatesRoot, opts.valuesFile)
			}
			if util.FileExists(opts.valuesFile) {
				opts.values, err = util.ParseJsonFile(opts.valuesFile)
				if err != nil {
					return err
				}
			}

			uid, err := uuid.NewUUID()
			if err != nil {
				return err
			}
			opts.uid = uid.String()

			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			err := opts.Run()
			if err != nil {
				ExitWithError("Diff", err)
			}
			color.New(color.Bold, color.FgGreen).Fprintf(os.Stdout, "\nDiff command has completed\n")
		},
	}

	cmd.Flags().StringVarP(&opts.manifestFile, "manifest", "m", "", "Set your manifest file")
	cmd.Flags().StringVarP(&opts.valuesFile, "values", "", "values.json", "Set your values file")
	cmd.Flags().IntVarP(&opts.workers, "workers", "w", worker.MaxWorker, "No of concurrent workers for fetching diff")
	cmd.Flags().StringVarP(&opts.profile, "profile", "", "default", "Profile to use from AWS credentials")
	cmd.Flags().StringVarP(&opts.role, "role", "", "", "Cloudformation service role to be used for stack operations")
	err := cmd.MarkFlagRequired("manifest")
	if err != nil {
		ExitWithError("diff", err)
	}

	return cmd
}

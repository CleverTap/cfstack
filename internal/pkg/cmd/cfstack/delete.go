package cfstack

import (
	"fmt"
	"github.com/CleverTap/cfstack/internal/pkg/aws/cloudformation"
	"github.com/CleverTap/cfstack/internal/pkg/aws/session"
	"github.com/CleverTap/cfstack/internal/pkg/manifest"
	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
)

type DeleteOpts struct {
	manifestFile string
	profile      string
	role         string

	uid           string
	templatesRoot string

	deleteStackOpts *DeleteStackOpts

	manifest manifest.Manifest
}

func (opts *DeleteOpts) preRun() error {
	templatesRoot, err := filepath.Abs(filepath.Dir(opts.manifestFile))
	if err != nil {
		return err
	}
	opts.templatesRoot = templatesRoot

	err = opts.manifest.Parse(opts.manifestFile)
	if err != nil {
		return err
	}

	uid, err := uuid.NewUUID()
	if err != nil {
		return err
	}
	opts.uid = uid.String()

	return nil
}

func (opts *DeleteOpts) Run() error {
	for _, region := range opts.manifest.Regions {
		stacks := region.Stacks
		sess, err := session.NewSession(&session.Opts{
			Profile: opts.profile,
			Region:  region.Name,
		})

		if err != nil {
			return err
		}

		deployer := cloudformation.NewWithoutValues(sess)

		for _, s := range stacks {
			fmt.Printf("==> %s  Deleting stack %s in region %s\n", knife, s.StackName, region.Name)
			s.SetRegion(region.Name)
			s.TemplateRootPath = opts.templatesRoot

			s.Deployer = deployer
			s.RoleArn = opts.role

			err := s.Delete()

			if err != nil {
				return err
			}
		}
	}
	return nil
}

func NewDeleteCmd() *cobra.Command {
	opts := &DeleteOpts{}
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete your cloudformation stacks",
		Long:  `Deletes cloudformation stacks defined in manifest files. Useful for tearing down stacks created in dev account`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return opts.preRun()
		},
		Run: func(cmd *cobra.Command, args []string) {
			err := opts.Run()
			if err != nil {
				ExitWithError("Delete", err)
			}
			color.New(color.Bold, color.FgGreen).Fprintf(os.Stdout, "\nDelete stacks command completed\n")
		},
	}

	cmd.PersistentFlags().StringVarP(&opts.manifestFile, "manifest", "m", "", "Set your manifest file")
	cmd.PersistentFlags().StringVarP(&opts.profile, "profile", "", "default", "Profile to use from AWS credentials")
	cmd.PersistentFlags().StringVarP(&opts.role, "role", "", "", "Cloudformation service role to be used for stack operations")
	cmd.AddCommand(opts.NewDeleteStackCmd())
	return cmd
}

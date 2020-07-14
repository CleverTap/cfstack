package cfstack

import (
	"fmt"
	"github.com/CleverTap/cfstack/internal/pkg/aws/cloudformation"
	"github.com/CleverTap/cfstack/internal/pkg/aws/session"
	"github.com/CleverTap/cfstack/internal/pkg/stack"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"os"
)

type DeleteStackOpts struct {
	name   string
	region string

	stack stack.Stack
}

func (opts *DeleteOpts) RunStackDelete() error {
	for _, region := range opts.manifest.Regions {
		if region.Name == opts.deleteStackOpts.region {
			for _, s := range region.Stacks {
				if s.StackName == opts.deleteStackOpts.name {
					fmt.Printf("==> %s  Deleting stack %s in region %s\n", knife, s.StackName, region.Name)
					sess, err := session.NewSession(&session.Opts{
						Profile: opts.profile,
						Region:  opts.deleteStackOpts.region,
					})

					if err != nil {
						return err
					}

					deployer := cloudformation.NewWithoutValues(sess)

					s.SetRegion(region.Name)
					s.TemplateRootPath = opts.templatesRoot

					s.Deployer = deployer
					s.RoleArn = opts.role

					return s.Delete()
				}
			}
		}
	}
	return fmt.Errorf("%s stack from %s region was not found in manifest file %s", opts.deleteStackOpts.name, opts.deleteStackOpts.region, opts.manifestFile)
}

func (opts *DeleteOpts) NewDeleteStackCmd() *cobra.Command {
	opts.deleteStackOpts = &DeleteStackOpts{}
	cmd := &cobra.Command{
		Use:     "stack",
		Aliases: []string{"service"},
		Short:   "Delete a single stack",
		Long:    `Delete specific stack using this command by passing the stack name along with manifest`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return opts.preRun()
		},
		Run: func(cmd *cobra.Command, args []string) {
			err := opts.RunStackDelete()
			if err != nil {
				ExitWithError("Delete stack", err)
			}
			color.New(color.Bold, color.FgGreen).Fprintf(os.Stdout, "\nDelete stack command completed\n")
		},
	}

	cmd.Flags().StringVarP(&opts.deleteStackOpts.name, "name", "n", "", "Name of the stack to be deployed")
	cmd.Flags().StringVarP(&opts.deleteStackOpts.region, "region", "r", "", "Region in which stack is to be deployed")
	err := cmd.MarkFlagRequired("name")
	if err != nil {
		ExitWithError("Deploy stack", err)
	}

	err = cmd.MarkFlagRequired("region")
	if err != nil {
		ExitWithError("Deploy stack", err)
	}

	return cmd
}

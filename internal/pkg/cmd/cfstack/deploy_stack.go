package cfstack

import (
	"errors"
	"fmt"
	"github.com/CleverTap/cfstack/internal/pkg/aws/cloudformation"
	"github.com/CleverTap/cfstack/internal/pkg/aws/s3"
	"github.com/CleverTap/cfstack/internal/pkg/aws/session"
	"github.com/CleverTap/cfstack/internal/pkg/stack"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/fatih/color"
	"github.com/golang/glog"
	"github.com/spf13/cobra"
	"os"
	"strings"
	"time"
)

type DeployStackOpts struct {
	name   string
	region string

	stack stack.Stack
}

func (opts *DeployOpts) RunStackDeploy() error {
	for _, region := range opts.manifest.Regions {
		if region.Name == opts.deployStackOpts.region {
			for _, s := range region.Stacks {
				if s.StackName == opts.deployStackOpts.name {
					fmt.Printf("==> %s  Deploying stack %s in region %s\n", rocket, s.StackName, region.Name)
					sess, err := session.NewSession(&session.Opts{
						Profile: opts.profile,
						Region:  opts.deployStackOpts.region,
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

					s.SetRegion(opts.deployStackOpts.region)
					s.SetUuid(opts.uid)
					s.SetBucket(bucket)
					s.TemplateRootPath = opts.templatesRoot

					s.Uploader = uploader
					s.Deployer = deployer

					s.RoleArn = opts.role

					timeout := time.After(24 * time.Hour)
					ticker := time.Tick(10 * time.Second)

					deployComplete := false

					for deployComplete == false {
						select {
						case <-timeout:
							return errors.New("too many AWS API calls. Try again later")
						case <-ticker:
							err = s.Deploy()
							if err != nil {
								if aerr, ok := err.(awserr.Error); ok {
									switch aerr.Code() {
									case "Throttling":
										glog.Warningf("AWS rate limit error for stack %s, retrying..", s.StackName)
										continue
									case "ValidationError":
										if strings.Contains(aerr.Message(), "S3 error: Access Denied") {
											glog.Warningf("AWS request error for stack %s, retrying..", s.StackName)
											continue
										}
									case "RequestError":
										glog.Warningf("AWS request error for stack %s -  %s, retrying..", s.StackName, aerr.Message())
										continue
									case "ChangeSetNotFound":
										glog.Warningf("Looks like changeset not found for stack %s, retrying..", s.StackName)
										continue
									default:
										glog.Errorf("unhandled AWS error for stack %s\n%s : %s", s.StackName, aerr.Code(), aerr.Message())
									}
								}
							}
							deployComplete = true
						}
					}
					if err != nil {
						fmt.Fprintf(os.Stdout, color.RedString("    %v\n", err))
						return fmt.Errorf("%s stack deployment has failed", s.StackName)
					}
					return nil
				}
			}
		}
	}
	return fmt.Errorf("%s stack from %s region was not found in manifest file %s", opts.deployStackOpts.name, opts.deployStackOpts.region, opts.manifestFile)
}

func (opts *DeployOpts) NewDeployStackCmd() *cobra.Command {
	opts.deployStackOpts = &DeployStackOpts{}
	cmd := &cobra.Command{
		Use:     "stack",
		Aliases: []string{"service"},
		Short:   "Deploy a single stack",
		Long:    `Deploy specific stack using this command by passing the stack name along with manifest`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return opts.preRun()
		},
		Run: func(cmd *cobra.Command, args []string) {
			err := opts.RunStackDeploy()
			if err != nil {
				ExitWithError("Deploy stack", err)
			}
			color.New(color.Bold, color.FgGreen).Fprintf(os.Stdout, "\nDeploy stack command completed\n")
		},
	}

	cmd.Flags().StringVarP(&opts.deployStackOpts.name, "name", "n", "", "Name of the stack to be deployed")
	cmd.Flags().StringVarP(&opts.deployStackOpts.region, "region", "r", "", "Region in which stack is to be deployed")
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

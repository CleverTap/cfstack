package cfstack

import (
	"encoding/json"
	"fmt"
	"github.com/CleverTap/cfstack/internal/pkg/aws/cloudformation"
	"github.com/CleverTap/cfstack/internal/pkg/aws/session"
	"github.com/CleverTap/cfstack/internal/pkg/templates"
	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"os"
)

type InitOpts struct {
	profile string
	region  string

	deployer cloudformation.CloudFormation
}

func (opts *InitOpts) Run() error {

	templateBody, err := templates.GenerateInitTemplate()

	if err != nil {
		return err
	}

	//fmt.Println(templateBody)

	stackPolicy := map[string]interface{}{
		"Statement": []map[string]string{
			{
				"Effect":    "Allow",
				"Action":    "Update:*",
				"Principal": "*",
				"Resource":  "*",
			},
		},
	}

	s, err := json.Marshal(stackPolicy)

	if err != nil {
		return err
	}

	stackName := "cfstack-Init"

	sess, err := session.NewSession(&session.Opts{
		Profile: opts.profile,
		Region:  opts.region,
	})

	if err != nil {
		return err
	}

	opts.deployer = cloudformation.NewWithoutValues(sess)

	fmt.Printf("    Checking if %s already exists in %s \n", stackName, opts.region)
	stackExists, err := opts.deployer.StackExists(stackName)

	if err != nil {
		return err
	}

	if stackExists {
		fmt.Printf("    %s stack found in %s. Checking for changes\n", stackName, opts.region)

		uid, err := uuid.NewUUID()
		if err != nil {
			return err
		}

		changes, err := opts.deployer.GetStackChanges(&cloudformation.GetStackChangesOpts{
			StackName:     stackName,
			TemplateBody:  templateBody,
			StackPolicy:   string(s),
			ChangeSetName: fmt.Sprintf("changeset-%s-%s", uid.String(), stackName),
			Type:          "UPDATE",
		})

		if err != nil {
			return err
		}

		if changes.StackPolicyChange == true {
			fmt.Printf("    Changes in stack policy detected, it will be updated first\n")
			err = opts.deployer.SetStackPolicy(stackName, string(s))

			if err != nil {
				return err
			}

			fmt.Printf("    Stack policy updated")
		}

		if len(changes.Resources) == 0 {
			fmt.Printf("    No resource changes detected in stack %s\n", stackName)
			return nil
		}

		err = opts.deployer.UpdateExistingStack(&cloudformation.CreateStackOpts{
			StackName:    stackName,
			TemplateBody: templateBody,
			StackPolicy:  string(s),
		})

		if err != nil {
			return err
		}
		fmt.Printf("    %s stack has been updated\n", stackName)

	} else {
		fmt.Printf("   %s not found in %s. Creating new stack\n", stackName, opts.region)
		err = opts.deployer.CreateNewStack(&cloudformation.CreateStackOpts{
			StackName:    stackName,
			TemplateBody: templateBody,
			StackPolicy:  string(s),
		})
		if err != nil {
			return err
		}
		fmt.Printf("    %s stack has been created\n", stackName)
	}

	return nil
}

func NewInitCmd() *cobra.Command {
	opts := &InitOpts{}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Init your region to be able use cfstack commands",
		Long: `Deploys a cfstack-Init cloudformation stack with required resources 
			(buckets, iam roles etc) needed to run cfstack commands.`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("==> %s  Initializing your account to run cfstack in %s\n", gear, opts.region)
			err := opts.Run()
			if err != nil {
				ExitWithError("init", err)
			}
			color.New(color.Bold, color.FgGreen).Fprintf(os.Stdout, "\n%s Initialization complete in %s\n", check, opts.region)
		},
	}

	cmd.Flags().StringVarP(&opts.region, "region", "r", "", "AWS Region to init cfstack in")
	cmd.Flags().StringVarP(&opts.profile, "profile", "", "default", "Profile to use from AWS credentials")
	err := cmd.MarkFlagRequired("region")
	if err != nil {
		ExitWithError("init", err)
	}

	return cmd
}

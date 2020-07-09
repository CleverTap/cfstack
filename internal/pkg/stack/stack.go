package stack

import (
	"encoding/json"
	"fmt"
	"git.wizrocket.net/infra/cfstack/internal/pkg/aws/cloudformation"
	"git.wizrocket.net/infra/cfstack/internal/pkg/aws/s3"
	"git.wizrocket.net/infra/cfstack/internal/pkg/templates"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/fatih/color"
	"github.com/golang/glog"
	"os"
	"path/filepath"
	"strings"
)

const (
	DiffSuccessStatus = "success"
	DiffFailStatus    = "failed"
	DiffUnknownStatus = "unknown"
)

type Stack struct {
	StackName               string                   `validate:"required",json:"StackName"`
	TemplatePath            string                   `validate:"required",json:"TemplatePath"`
	TemplateRootPath        string                   `json:"TemplateRootPath"`
	AbsTemplatePath         string                   `json:"AbsTemplatePath"`
	TemplateUrl             string                   `json:"TemplateUrl"`
	Action                  string                   `validate:"required",json:"Action"`
	StackPolicy             templates.PolicyDocument `validate:"required",json:"StackPolicy"`
	Region                  string                   `json:"Region"`
	UID                     string                   `json:"UID,omitempty"`
	Bucket                  string                   `json:"Bucket,omitempty"`
	Parameters              map[string]string        `validate:"required",json:"Parameters"`
	DeploymentOrder         int                      `json:"DeploymentOrder"`
	Changes                 *cloudformation.Changes

	SuppressMessages bool

	serverless bool
	RoleArn    string

	Deployer cloudformation.CloudFormation
	Uploader s3.S3
}

func (s *Stack) SetRegion(region string) {
	s.Region = region
}

func (s *Stack) SetDeploymentOrder(i int) {
	s.DeploymentOrder = i
}

func (s *Stack) SetBucket(bucket string) {
	s.Bucket = bucket
}

func (s *Stack) SetUuid(uuid string) {
	s.UID = uuid
}

func (s *Stack) getChangeSetName() string {
	return fmt.Sprintf("changeset-%s-%s", s.UID, s.StackName)
}

func (s *Stack) Deploy() error {

	if len(s.TemplateUrl) == 0 {
		err := s.uploadTemplate()
		if err != nil {
			return err
		}
	}

	err := s.Deployer.ValidateTemplate(s.TemplateUrl)

	if err != nil {
		return err
	}

	stackExists, err := s.Deployer.StackExists(s.StackName)

	if err != nil {
		return err
	}

	if s.Action == "DELETE" {
		if stackExists {
			err = s.delete()
			if err != nil {
				return err
			}
		}
		fmt.Printf("There is no stack %s in region %s to delete\n", s.StackName, s.Region)
	} else {
		if stackExists {
			err = s.update()
			if err != nil {
				return err
			}
		} else {
			err = s.create()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Stack) Diff() error {
	if len(s.TemplateUrl) == 0 {
		err := s.uploadTemplate()
		if err != nil {
			return err
		}
	}

	err := s.Deployer.ValidateTemplate(s.TemplateUrl)

	if err != nil {
		glog.Warningf("Template validation error for stack %s", s.StackName)
		return err
	}

	var changeSetType string

	stackExists, err := s.Deployer.StackExists(s.StackName)

	if err != nil {
		return err
	}

	if stackExists {
		changeSetType = "UPDATE"
	} else {
		changeSetType = "CREATE"
	}

	stackPolicy, err := json.Marshal(s.StackPolicy)

	if err != nil {
		return err
	}

	getStackChangesOpts := cloudformation.GetStackChangesOpts{
		StackName:     s.StackName,
		TemplateUrl:   s.TemplateUrl,
		StackPolicy:   string(stackPolicy),
		Parameters:    s.Parameters,
		ChangeSetName: s.getChangeSetName(),
		Type:          changeSetType,
		RoleArn:       s.RoleArn,
	}

	changes, err := s.Deployer.GetStackChanges(&getStackChangesOpts)

	if err != nil {
		return err
	}

	s.Changes = changes

	return nil
}

func (s *Stack) Delete() error {
	stackExists, err := s.Deployer.StackExists(s.StackName)

	if err != nil {
		return err
	}

	if stackExists {
		return s.delete()
	}
	color.New(color.FgYellow).Fprintf(os.Stdout, "    Stack %s does not exist in region %s\n", s.StackName, s.Region)
	return nil
}

func (s *Stack) create() error {
	if !s.SuppressMessages {
		fmt.Printf("    Stack doesn't exist, creating a new one\n")
	}

	stackPolicy, err := json.Marshal(s.StackPolicy)
	if err != nil {
		return err
	}

	err = s.Deployer.CreateNewStack(&cloudformation.CreateStackOpts{
		StackName:   s.StackName,
		TemplateUrl: s.TemplateUrl,
		Parameters:  s.Parameters,
		StackPolicy: string(stackPolicy),
		Serverless:  s.serverless,
		RoleArn:     s.RoleArn,
	})
	if err != nil {
		return err
	}
	if !s.SuppressMessages {
		color.New(color.FgGreen).Fprintf(os.Stdout, "    Stack create complete\n")
	}
	return nil
}

func (s *Stack) update() error {
	if !s.SuppressMessages {
		fmt.Printf("    Stack exists, will check for updates\n")
	}

	stackPolicy, err := json.Marshal(s.StackPolicy)
	if err != nil {
		return err
	}

	changes, err := s.Deployer.GetStackChanges(&cloudformation.GetStackChangesOpts{
		StackName:     s.StackName,
		TemplateUrl:   s.TemplateUrl,
		StackPolicy:   string(stackPolicy),
		Parameters:    s.Parameters,
		ChangeSetName: s.getChangeSetName(),
		Type:          "UPDATE",
		RoleArn:       s.RoleArn,
	})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case "ValidationError":
				if strings.Contains(aerr.Message(), "IN_PROGRESS") {
					color.New(color.FgYellow).Fprintf(os.Stdout, "    %s\n", aerr.Message())
					return nil
				} else {
					return fmt.Errorf("Unhandled AWS ValidationError for stack %s\n%s", s.StackName, aerr.Message())
				}
			default:
				return fmt.Errorf("Unhandled AWS error for stack %s\n%s", s.StackName, aerr.Message())
			}
		}
		return err
	}

	if changes.StackPolicyChange == true && string(stackPolicy) != "{}" {
		if !s.SuppressMessages {
			fmt.Printf("    Changes in %s stack policy detected, it will be updated first\n", s.StackName)
		}
		err = s.Deployer.SetStackPolicy(s.StackName, string(stackPolicy))

		if err != nil {
			return err
		}
		if !s.SuppressMessages {
			color.New(color.FgGreen).Fprintf(os.Stdout, "    Stack upolicy updated\n")
		}
	}

	if len(changes.Resources) == 0 && changes.ForceStackUpdate == false {
		if !s.SuppressMessages {
			color.New(color.FgYellow).Fprintf(os.Stdout, "    No resource changes detected for stack, skipping update..\n")
		}
		return nil
	}

	if !s.SuppressMessages {
		fmt.Printf("    Changes in %s resources detected, waiting for update to finish\n", s.StackName)
	}

	err = s.Deployer.UpdateExistingStack(&cloudformation.CreateStackOpts{
		StackName:   s.StackName,
		TemplateUrl: s.TemplateUrl,
		Parameters:  s.Parameters,
		StackPolicy: string(stackPolicy),
		Serverless:  s.serverless,
		RoleArn:     s.RoleArn,
	})

	if err != nil {
		return err
	}
	if !s.SuppressMessages {
		color.New(color.FgGreen).Fprintf(os.Stdout, "    Stack update complete\n")
	}

	return nil
}

func (s *Stack) delete() error {
	err := s.Deployer.DeleteStack(&cloudformation.DeleteStackOpts{
		StackName: s.StackName,
		RoleArn:   s.RoleArn,
	})
	if err != nil {
		return err
	}
	color.New(color.FgGreen).Fprintf(os.Stdout, "    Stack delete complete\n")
	return nil
}

func (s *Stack) uploadTemplate() error {
	s.AbsTemplatePath = s.TemplatePath

	s.TemplateUrl = "https://s3-" + s.Region + ".amazonaws.com/" + s.Bucket + "/" + s.UID + s.TemplatePath

	if !filepath.IsAbs(s.TemplatePath) {
		s.AbsTemplatePath = filepath.Join(s.TemplateRootPath, s.TemplatePath)

		s.TemplateUrl = "https://s3-" + s.Region + ".amazonaws.com/" + s.Bucket + "/" + s.UID + "/" + s.TemplatePath
	}

	if s.Region == "us-east-1" {
		s.TemplateUrl = strings.Replace(s.TemplateUrl, "https://s3-", "https://s3.", -1)
	}

	isServerLessStack, err := templates.IsServerlessTemplate(s.AbsTemplatePath)

	if err != nil {
		return err
	}

	if isServerLessStack {
		s.serverless = true
		fmt.Printf("    Packing serverless stack %s\n", s.StackName)
		err = s.packageServerlessTemplate()

		if err != nil {
			return err
		}
	}

	uploaderOpts := s3.Opts{
		Bucket:   s.Bucket,
		Filepath: s.AbsTemplatePath,
		Key:      s.UID + "/" + s.TemplatePath,
	}

	err = s.Uploader.UploadToS3(&uploaderOpts)

	if err != nil {
		glog.Errorf("template upload for stack %s failed", s.StackName)
		return err
	}

	//if isServerLessStack {
	//	err = os.Remove(s.AbsTemplatePath)
	//	if err != nil {
	//		return err
	//	}
	//}

	return nil
}

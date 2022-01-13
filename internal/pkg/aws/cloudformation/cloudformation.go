package cloudformation

import (
	"encoding/json"
	"github.com/CleverTap/cfstack/internal/pkg/templates"
	"github.com/Jeffail/gabs"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/cloudformation/cloudformationiface"
	"github.com/fatih/color"
	"github.com/golang/glog"
	"github.com/pkg/errors"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	noChangeErrorReason = "The submitted information didn't contain changes. Submit different information to create a change set."
)

type CloudFormation struct {
	client cloudformationiface.CloudFormationAPI
	region string
	values *gabs.Container
}

type GetStackChangesOpts struct {
	StackName     string
	TemplateUrl   string
	TemplateBody  string
	Parameters    map[string]string
	StackPolicy   string
	ChangeSetName string
	Type          string
	RoleArn       string
}

type CreateStackOpts struct {
	StackName    string
	TemplateUrl  string
	TemplateBody string
	Parameters   map[string]string
	Serverless   bool
	StackPolicy  string
	RoleArn      string
}

type DeleteStackOpts struct {
	StackName string
	RoleArn   string
}

type Changes struct {
	Status            string
	StatusReason      string
	Resources         []ChangeResource
	StackPolicyChange bool
	ForceStackUpdate  bool
}

type ChangeResource struct {
	Name        string
	Type        string
	Action      string
	Replacement string
}

func New(sess *session.Session, v *gabs.Container) CloudFormation {
	return CloudFormation{
		client: cloudformation.New(sess),
		region: aws.StringValue(sess.Config.Region),
		values: v,
	}
}

func NewWithoutValues(sess *session.Session) CloudFormation {
	return CloudFormation{
		client: cloudformation.New(sess),
	}
}

func (cf CloudFormation) ValidateTemplate(templateUrl string) error {
	_, err := cf.client.ValidateTemplate(&cloudformation.ValidateTemplateInput{
		TemplateURL: aws.String(templateUrl),
	})

	return err
}

func (cf CloudFormation) GetStackResourcePhysicalId(stack string, resource string) (string, error) {

	stackExists, err := cf.StackExists(stack)

	if err != nil {
		return "", err
	}

	if !stackExists {
		return "", errors.Errorf("%s stack doesnt exist", stack)
	}

	res, err := cf.client.DescribeStackResource(&cloudformation.DescribeStackResourceInput{
		StackName:         aws.String(stack),
		LogicalResourceId: aws.String(resource),
	})

	if err != nil {
		return "", err
	}

	return aws.StringValue(res.StackResourceDetail.PhysicalResourceId), nil
}

func (cf CloudFormation) GetTemplateString(stack string) (string, error) {

	stackExists, err := cf.StackExists(stack)

	if err != nil {
		return "{}", err
	}

	if !stackExists {
		return "{}", nil
	}

	res, err := cf.client.GetTemplate(&cloudformation.GetTemplateInput{
		StackName:     aws.String(stack),
		TemplateStage: aws.String("Original"),
	})

	if err != nil {
		return "{}", err
	}

	return aws.StringValue(res.TemplateBody), nil
}

func (cf CloudFormation) StackExists(stackName string) (bool, error) {

	timeout := time.After(24 * time.Hour)
	ticker := time.Tick(10 * time.Second)

	stackExistsComplete := false

	for stackExistsComplete == false {
		select {
		case <-timeout:
			return false, errors.New("too many AWS API calls. Try again later")
		case <-ticker:
			res, err := cf.client.DescribeStacks(&cloudformation.DescribeStacksInput{
				StackName: aws.String(stackName),
			})

			if err != nil {
				if aerr, ok := err.(awserr.Error); ok {
					switch aerr.Code() {
					case "Throttling":
						glog.Warningf("AWS rate limit error for stack %s, retrying..", stackName)
						continue
					case "ValidationError":
						if strings.Contains(aerr.Message(), "S3 error: Access Denied") {
							glog.Warningf("AWS request error for stack %s, retrying..", stackName)
							continue
						}
						return false, nil
					case "RequestError":
						glog.Warningf("AWS request error for stack %s -  %s, retrying..", stackName, aerr.Message())
						continue
					default:
						return false, errors.Errorf("unhandled AWS error for stack %s\n%s : %s", stackName, aerr.Code(), aerr.Message())
					}
				}

				return false, err
			}

			for _, s := range res.Stacks {
				if aws.StringValue(s.StackName) == stackName && aws.StringValue(s.StackStatus) == cloudformation.StackStatusReviewInProgress {
					return false, nil
				}
			}
			stackExistsComplete = true
		}
	}

	return true, nil
}

func (cf *CloudFormation) ResolveParameterValue(stack string, parameter string) (string, error) {
	if !cf.values.Exists(cf.region, stack, parameter) {
		return "", errors.Errorf("Value for parameter %s not found for stack %s in region %s", parameter, stack, cf.region)
	}

	value, err := strconv.Unquote(cf.values.Search(cf.region, stack, parameter).String())
	if err != nil {
		return "", err
	}

	return value, nil
}

func (cf CloudFormation) GetStackChanges(opts *GetStackChangesOpts) (*Changes, error) {
	var stackPolicyChange bool
	var forceStackUpdate bool
	var resources []ChangeResource

	var parameters []*cloudformation.Parameter

	for k, v := range opts.Parameters {
		if len(v) >= 4 && v[0:2] == "{{" && v[len(v)-2:] == "}}" {
			valueName := strings.TrimSpace(v[2 : len(v)-2])
			if !cf.values.Exists(cf.region, opts.StackName, valueName) {
				return nil, errors.Errorf("Value %s for parameter %s not found in values", valueName, k)
			}
			value, err := strconv.Unquote(cf.values.Search(cf.region, opts.StackName, valueName).String())
			if err != nil {
				return nil, err
			}
			v = value
		}
		parameters = append(parameters, &cloudformation.Parameter{
			ParameterKey:   aws.String(k),
			ParameterValue: aws.String(v),
		})
	}

	createChangeSetInput := &cloudformation.CreateChangeSetInput{
		StackName:     aws.String(opts.StackName),
		ChangeSetName: aws.String(opts.ChangeSetName),
		ChangeSetType: aws.String(opts.Type),
		Parameters:    parameters,
		Capabilities: []*string{
			aws.String("CAPABILITY_NAMED_IAM"),
		},
	}

	if opts.TemplateUrl == "" {
		createChangeSetInput.TemplateBody = aws.String(opts.TemplateBody)
	} else {
		createChangeSetInput.TemplateURL = aws.String(opts.TemplateUrl)
	}

	if opts.RoleArn != "" {
		createChangeSetInput.RoleARN = aws.String(opts.RoleArn)
	}

	if opts.Type == "UPDATE" {

		res, err := cf.client.GetStackPolicy(&cloudformation.GetStackPolicyInput{
			StackName: aws.String(opts.StackName),
		})

		if err != nil {
			return nil, err
		}

		stackPolicy := templates.PolicyDocument{}

		err = json.Unmarshal([]byte(aws.StringValue(res.StackPolicyBody)), &stackPolicy)

		s, err := json.Marshal(stackPolicy)
		if err != nil {
			return nil, err
		}

		if opts.StackPolicy != string(s) && opts.StackPolicy != "{}" {
			stackPolicyChange = true
		}
	}

	_, err := cf.client.CreateChangeSet(createChangeSetInput)

	if err != nil {
		return nil, err
	}

	resources, forceStackUpdate, err = cf.trackChangeSetCreateStatus(opts.StackName, opts.ChangeSetName)

	if err != nil {
		return nil, err
	}

	_, err = cf.client.DeleteChangeSet(&cloudformation.DeleteChangeSetInput{
		ChangeSetName: aws.String(opts.ChangeSetName),
		StackName:     aws.String(opts.StackName),
	})

	return &Changes{
		Resources:         resources,
		StackPolicyChange: stackPolicyChange,
		ForceStackUpdate:  forceStackUpdate,
	}, nil
}

func (cf CloudFormation) trackChangeSetCreateStatus(stackName string, changeSetName string) ([]ChangeResource, bool, error) {
	resources := make([]ChangeResource, 0)

	timeout := time.After(24 * time.Hour)
	ticker := time.Tick(30 * time.Second)

	for {
		select {
		case <-timeout:
			return nil, false, errors.New("Stack %s failed to update/create within 24 hours...")
		case <-ticker:
			res, err := cf.client.DescribeChangeSet(&cloudformation.DescribeChangeSetInput{
				ChangeSetName: aws.String(changeSetName),
				StackName:     aws.String(stackName),
			})

			if err != nil {
				if aerr, ok := err.(awserr.Error); ok {
					switch aerr.Code() {
					case "Throttling":
						glog.Warningf("AWS rate limit error while describing changeset for stack %s, retrying..", stackName)
						continue
					default:
						return nil, false, err
					}
				}

				return nil, false, err
			}

			switch aws.StringValue(res.Status) {
			case cloudformation.ChangeSetStatusFailed:
				switch aws.StringValue(res.StatusReason) {
				case noChangeErrorReason:
					return resources, false, nil
				default:
					return nil, false, errors.New(aws.StringValue(res.StatusReason))
				}
			case cloudformation.ChangeSetStatusCreateComplete:
				if len(res.Changes) == 0 {
					return resources, true, nil
				}
				for _, change := range res.Changes {
					resources = append(resources, ChangeResource{
						Name:        aws.StringValue(change.ResourceChange.LogicalResourceId),
						Type:        aws.StringValue(change.ResourceChange.ResourceType),
						Action:      aws.StringValue(change.ResourceChange.Action),
						Replacement: aws.StringValue(change.ResourceChange.Replacement),
					})
				}
				return resources, false, nil
			}
		}
	}
}

func (cf CloudFormation) SetStackPolicy(stackName string, stackPolicy string) error {
	updateStackInput := &cloudformation.SetStackPolicyInput{
		StackName:       aws.String(stackName),
		StackPolicyBody: aws.String(stackPolicy),
	}

	_, err := cf.client.SetStackPolicy(updateStackInput)

	if err != nil {
		return err
	}

	return nil
}

func (cf CloudFormation) UpdateExistingStack(opts *CreateStackOpts) error {
	var parameters []*cloudformation.Parameter

	for k, v := range opts.Parameters {
		if len(v) >= 4 && v[0:2] == "{{" && v[len(v)-2:] == "}}" {
			valueName := strings.TrimSpace(v[2 : len(v)-2])
			if !cf.values.Exists(cf.region, opts.StackName, valueName) {
				return errors.Errorf("Value %s for parameter %s not found in values", valueName, k)
			}
			value, err := strconv.Unquote(cf.values.Search(cf.region, opts.StackName, valueName).String())
			if err != nil {
				return err
			}
			v = value
		}
		parameters = append(parameters, &cloudformation.Parameter{
			ParameterKey:   aws.String(k),
			ParameterValue: aws.String(v),
		})
	}

	capabilities := []*string{
		aws.String("CAPABILITY_NAMED_IAM"),
	}

	if opts.Serverless {
		capabilities = append(capabilities, aws.String("CAPABILITY_AUTO_EXPAND"))
	}

	updateStackInput := &cloudformation.UpdateStackInput{
		StackName:    aws.String(opts.StackName),
		Parameters:   parameters,
		Capabilities: capabilities,
	}

	if opts.TemplateUrl == "" {
		updateStackInput.TemplateBody = aws.String(opts.TemplateBody)

	} else {
		updateStackInput.TemplateURL = aws.String(opts.TemplateUrl)
	}

	if opts.RoleArn != "" {
		updateStackInput.RoleARN = aws.String(opts.RoleArn)
	}

	timeout := time.After(24 * time.Hour)
	ticker := time.Tick(15 * time.Second)

	updateStackCalled := false

	for updateStackCalled == false {
		select {
		case <-timeout:
			return errors.New("too many AWS API calls. Try again later")
		case <-ticker:
			_, err := cf.client.UpdateStack(updateStackInput)

			if err != nil {
				if aerr, ok := err.(awserr.Error); ok {
					switch aerr.Code() {
					case "Throttling":
						glog.Warningf("AWS rate limit error for stack %s, retrying..", opts.StackName)
						continue
					case "RequestError":
						glog.Warningf("AWS request error for stack %s, retrying..", opts.StackName)
						continue
					default:
						return errors.Errorf("unhandled AWS error for stack %s\n%s : %s", opts.StackName, aerr.Code(), aerr.Message())
					}
				}
				return err
			}
			updateStackCalled = true
		}
	}

	err := cf.trackStackCreateUpdateStatus(opts.StackName)

	if err != nil {
		return err
	}

	return nil
}

func (cf CloudFormation) DeleteStack(opts *DeleteStackOpts) error {
	deleteStackInput := &cloudformation.DeleteStackInput{
		StackName: aws.String(opts.StackName),
	}

	if opts.RoleArn != "" {
		deleteStackInput.RoleARN = aws.String(opts.RoleArn)
	}

	timeout := time.After(24 * time.Hour)
	ticker := time.Tick(15 * time.Second)

	deleteStackCalled := false

	for deleteStackCalled == false {
		select {
		case <-timeout:
			return errors.New("too many AWS API calls. Try again later")
		case <-ticker:
			_, err := cf.client.DeleteStack(deleteStackInput)

			if err != nil {
				if aerr, ok := err.(awserr.Error); ok {
					switch aerr.Code() {
					case "Throttling":
						glog.Warningf("AWS rate limit error for stack %s, retrying..", opts.StackName)
						continue
					case "RequestError":
						glog.Warningf("AWS request error for stack %s, retrying..", opts.StackName)
						continue
					default:
						return errors.Errorf("unhandled AWS error for stack %s\n%s : %s", opts.StackName, aerr.Code(), aerr.Message())
					}
				}
				return err
			}
			deleteStackCalled = true
		}
	}

	err := cf.trackStackCreateUpdateStatus(opts.StackName)

	if err != nil {
		return err
	}
	return nil
}

func (cf CloudFormation) CreateNewStack(opts *CreateStackOpts) error {

	var parameters []*cloudformation.Parameter

	for k, v := range opts.Parameters {
		if len(v) >= 4 && v[0:2] == "{{" && v[len(v)-2:] == "}}" {
			valueName := strings.TrimSpace(v[2 : len(v)-2])
			if !cf.values.Exists(cf.region, opts.StackName, valueName) {
				return errors.Errorf("Value %s for parameter %s not found in values", valueName, k)
			}
			value, err := strconv.Unquote(cf.values.Search(cf.region, opts.StackName, valueName).String())
			if err != nil {
				return err
			}
			v = value
		}
		parameters = append(parameters, &cloudformation.Parameter{
			ParameterKey:   aws.String(k),
			ParameterValue: aws.String(v),
		})
	}

	capabilities := []*string{
		aws.String("CAPABILITY_NAMED_IAM"),
	}

	if opts.Serverless {
		capabilities = append(capabilities, aws.String("CAPABILITY_AUTO_EXPAND"))
	}

	createStackInput := &cloudformation.CreateStackInput{
		StackName:    aws.String(opts.StackName),
		Parameters:   parameters,
		Capabilities: capabilities,
	}

	if opts.StackPolicy != "" && opts.StackPolicy != "{}" {
		createStackInput.StackPolicyBody = aws.String(opts.StackPolicy)
	}

	if opts.TemplateUrl == "" {
		createStackInput.TemplateBody = aws.String(opts.TemplateBody)

	} else {
		createStackInput.TemplateURL = aws.String(opts.TemplateUrl)
	}

	if opts.RoleArn != "" {
		createStackInput.RoleARN = aws.String(opts.RoleArn)
	}

	timeout := time.After(24 * time.Hour)
	ticker := time.Tick(15 * time.Second)

	createStackCalled := false

	for createStackCalled == false {
		select {
		case <-timeout:
			return errors.New("too many AWS API calls. Try again later")
		case <-ticker:
			_, err := cf.client.CreateStack(createStackInput)

			if err != nil {
				if aerr, ok := err.(awserr.Error); ok {
					switch aerr.Code() {
					case "Throttling":
						glog.Warningf("AWS rate limit error for stack %s, retrying..", opts.StackName)
						continue
					case "RequestError":
						glog.Warningf("AWS request error for stack %s, retrying..", opts.StackName)
						continue
					default:
						return errors.Errorf("unhandled AWS error for stack %s\n%s : %s", opts.StackName, aerr.Code(), aerr.Message())
					}
				}
				return err
			}
			createStackCalled = true
		}
	}

	err := cf.trackStackCreateUpdateStatus(opts.StackName)

	if err != nil {
		res, err1 := cf.client.DescribeStacks(&cloudformation.DescribeStacksInput{
			StackName: aws.String(opts.StackName),
		})

		if err1 != nil {
			return err1
		}

		for _, s := range res.Stacks {
			if aws.StringValue(s.StackName) == opts.StackName && aws.StringValue(s.StackStatus) == cloudformation.StackStatusRollbackComplete {
				color.New(color.FgRed).Fprintf(os.Stdout, "    Deleting stack\n")
				err2 := cf.DeleteStack(&DeleteStackOpts{
					StackName: opts.StackName,
				})

				if err2 != nil {
					return err2
				}
			}
		}
		return err
	}

	return nil
}

func (cf CloudFormation) trackStackCreateUpdateStatus(stackName string) error {
	completed := false
	var currentStatus string

	timeout := time.After(24 * time.Hour)
	ticker := time.Tick(5 * time.Second)

	for completed == false {
		select {
		case <-timeout:
			return errors.Errorf("Stack %s failed to update/create within 24 hours...", stackName)
		case <-ticker:
			res, err := cf.client.DescribeStacks(&cloudformation.DescribeStacksInput{
				StackName: aws.String(stackName),
			})

			if err != nil {
				if aerr, ok := err.(awserr.Error); ok {
					switch aerr.Code() {
					case "Throttling":
						glog.Warningf("AWS rate limit error %s. retrying for stack %s", aerr.Error(), stackName)
						continue
					case "RequestError":
						glog.Warningf("AWS request error for stack %s, retrying..", stackName)
						continue
					case "ValidationError":
						// This can happen if the stack has already been deleted by the time we check this
						// So we exit the loop
						if strings.Contains(aerr.Message(), "does not exist") {
							color.New(color.FgYellow).Fprintf(os.Stdout, "    Completed deleting stack\n")
							return nil
						}
					default:
						return err
					}
				}

				return err
			}

			//fmt.Println(res.String())

			for _, stack := range res.Stacks {
				if aws.StringValue(stack.StackName) == stackName {
					switch aws.StringValue(stack.StackStatus) {
					case cloudformation.StackStatusCreateInProgress:
						if currentStatus != cloudformation.StackStatusCreateInProgress {
							glog.Infof("Creating %s stack", stackName)
							currentStatus = cloudformation.StackStatusCreateInProgress
						}
					case cloudformation.StackStatusRollbackInProgress:
						if currentStatus != cloudformation.StackStatusRollbackInProgress {
							color.New(color.FgRed).Fprintf(os.Stdout, "    Failed to create stack, rolling back\n")
							color.New(color.FgRed).Fprintf(os.Stdout, "    Rollback reason: %s\n", aws.StringValue(stack.StackStatusReason))
							currentStatus = cloudformation.StackStatusRollbackInProgress
						}
					case cloudformation.StackStatusRollbackComplete:
						return errors.Errorf("Failed to create stack %s", stackName)
					case cloudformation.StackStatusCreateFailed:
						return errors.Errorf("Failed to create stack %s\nReason: %s\n", stackName, aws.StringValue(stack.StackStatusReason))
					case cloudformation.StackStatusUpdateInProgress:
						if currentStatus != cloudformation.StackStatusUpdateInProgress {
							glog.Infof("Updating stack %s\n", stackName)
							currentStatus = cloudformation.StackStatusUpdateInProgress
						}
					case cloudformation.StackStatusUpdateCompleteCleanupInProgress:
						if currentStatus != cloudformation.StackStatusUpdateInProgress {
							glog.Infof("Updating stack %s\n", stackName)
							currentStatus = cloudformation.StackStatusUpdateInProgress
						}
					case cloudformation.StackStatusUpdateRollbackInProgress:
						if currentStatus != cloudformation.StackStatusUpdateRollbackInProgress {
							color.New(color.FgRed).Fprintf(os.Stdout, "    Failed to update stack, rolling back\n")
							color.New(color.FgRed).Fprintf(os.Stdout, "    Rollback reason: %s\n", aws.StringValue(stack.StackStatusReason))
							currentStatus = cloudformation.StackStatusUpdateRollbackInProgress
						}
					case cloudformation.StackStatusUpdateRollbackCompleteCleanupInProgress:
						if currentStatus != cloudformation.StackStatusUpdateRollbackInProgress {
							glog.Errorf("Rolling back stack %s update\n", stackName)
							currentStatus = cloudformation.StackStatusUpdateRollbackInProgress
						}
					case cloudformation.StackStatusUpdateRollbackComplete:
						return errors.Errorf("Failed to update stack %s", stackName)
					case cloudformation.StackStatusUpdateRollbackFailed:
						return errors.Errorf("Failed to rollback stack %s\nReason: %s", stackName, aws.StringValue(stack.StackStatusReason))
					case cloudformation.StackStatusDeleteInProgress:
						if currentStatus != cloudformation.StackStatusDeleteInProgress {
							currentStatus = cloudformation.StackStatusDeleteInProgress
						}
					case cloudformation.StackStatusDeleteFailed:
						return errors.Errorf("Failed to delete stack %s\nReason: %s", stackName, aws.StringValue(stack.StackStatusReason))
					case cloudformation.StackStatusCreateComplete:
						glog.Infof("Completed creating stack %s", stackName)
						completed = true
					case cloudformation.StackStatusUpdateComplete:
						glog.Infof("Completed updating stack %s", stackName)
						completed = true
					case cloudformation.StackStatusDeleteComplete:
						glog.Errorf("Completed deleting stack %s", stackName)
						completed = true
					}

				}
			}
		}
	}

	return nil
}

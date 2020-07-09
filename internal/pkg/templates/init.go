package templates

import (
	"github.com/awslabs/goformation/v3/cloudformation"
	"github.com/awslabs/goformation/v3/cloudformation/iam"
	"github.com/awslabs/goformation/v3/cloudformation/s3"
)

func GenerateInitTemplate() (string, error) {
	sTemplate := cloudformation.NewTemplate()

	sourceS3Bucket := &s3.Bucket{
		AccessControl: "BucketOwnerFullControl",
		VersioningConfiguration: &s3.Bucket_VersioningConfiguration{
			Status: "Enabled",
		},
	}

	sTemplate.Resources["SourceS3Bucket"] = sourceS3Bucket

	templatesS3Bucket := &s3.Bucket{
		AccessControl: "BucketOwnerFullControl",
		VersioningConfiguration: &s3.Bucket_VersioningConfiguration{
			Status: "Enabled",
		},
	}

	sTemplate.Resources["TemplatesS3Bucket"] = templatesS3Bucket

	cloudFormationServiceIamRole := &iam.Role{
		AssumeRolePolicyDocument: PolicyDocument{
			Statement: []Statement{
				{
					Sid:    "RoleToBeAssumedByCfstackToExecuteCloudformationFuncitons",
					Effect: "Allow",
					Principal: map[string]interface{}{
						"Service": "cloudformation.amazonaws.com",
					},
					Action: "sts:AssumeRole",
				},
			},
		},
		Path: "/",
	}

	cloudFormationServiceIamPolicy := &iam.Policy{
		PolicyDocument: PolicyDocument{
			Statement: []Statement{
				{
					Sid:    "AllowInteractionWithEcsCluster",
					Effect: "Allow",
					Action: []string{
						"*",
					},
					Resource: "*",
				},
			},
		},

		PolicyName: cloudformation.Join("-", []string{
			cloudformation.Ref("AWS::StackName"),
			"CloudFormationServiceIamPolicy",
		}),
		Roles: []string{
			cloudformation.Ref("CloudFormationServiceIamRole"),
		},
	}

	sTemplate.Resources["CloudFormationServiceIamRole"] = cloudFormationServiceIamRole

	sTemplate.Resources["CloudFormationServiceIamPolicy"] = cloudFormationServiceIamPolicy

	j, err := sTemplate.JSON()

	if err != nil {
		return "", nil
	}

	return string(j), nil
}

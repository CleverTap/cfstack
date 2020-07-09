package templates

import (
	"github.com/wizrocket/goformation"
)

func IsServerlessTemplate(templatePath string) (bool, error) {
	template, err := goformation.Open(templatePath)

	if err != nil {
		return false, err
	}

	if template.Transform == nil {
		return false, nil
	}

	j, err := template.Transform.MarshalJSON()

	if err != nil {
		return false, err
	}
	transform := cleanupString(string(j))

	functions := template.GetAllAWSServerlessFunctionResources()

	if transform == "AWS::Serverless-2016-10-31" && len(functions) > 0 {
		return true, nil
	}

	return false, nil
}

func cleanupString(s string) string {
	return s[1 : len(s)-1]
}

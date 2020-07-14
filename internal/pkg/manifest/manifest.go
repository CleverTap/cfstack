package manifest

import (
	"encoding/json"
	"fmt"
	"git.wizrocket.net/infra/cfstack/internal/pkg/stack"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/golang/glog"
	"github.com/pkg/errors"
	"gopkg.in/go-playground/validator.v9"
	"io/ioutil"
	"os"
	"path/filepath"
)

type Region struct {
	Name   string        `validate:"required" json:"Name"`
	Stacks []stack.Stack `validate:"required" json:"Stacks"`
}

type Manifest struct {
	Regions            []Region `validate:"required" json:"Regions"`
	ParallelDeployment bool     `json:"ParallelDeployment"`
}

func (manifest *Manifest) Parse(file string) error {
	manifestFileName := filepath.Base(file)

	page := "📃"

	fmt.Printf("==> %s  Parsing manifest file %s\n", page, manifestFileName)

	manifestFileBasePath, err := filepath.Abs(filepath.Dir(file))

	if err != nil {
		return err
	}

	jsonFile, err := os.Open(filepath.Join(manifestFileBasePath, manifestFileName))

	if err != nil {
		return err
	}
	defer jsonFile.Close()

	byteValue, _ := ioutil.ReadAll(jsonFile)
	err = json.Unmarshal(byteValue, &manifest)
	if err != nil {
		return err
	}

	return manifest.validateManifestFile()
}

func (manifest *Manifest) validateManifestFile() error {
	resolver := endpoints.DefaultResolver()
	partitions := resolver.(endpoints.EnumPartitions).Partitions()
	var cnt int

	if len(manifest.Regions) == 0 {
		return errors.Errorf("No Regions found")
	}

	validate := validator.New()
	err := validate.Struct(manifest)
	if err != nil {
		glog.Errorf("Manifest validation error %v", err)
		if _, ok := err.(*validator.InvalidValidationError); ok {
			os.Exit(1)

		}
	}

	for i, region := range manifest.Regions {
		err := validate.Struct(region)
		if err != nil {
			for _, e := range err.(validator.ValidationErrors) {
				if e.Field() == "Name" {
					return errors.Errorf("Region name is missing for %d element", i)
				} else {
					return errors.Errorf("Missing field %s for region %s", e.Field(), region.Name)
				}
			}
		}

		cnt = 0
		for _, p := range partitions {
			for _, pr := range p.Regions() {
				if region.Name == pr.ID() {
					cnt++
				}
			}
		}
		if cnt == 0 {
			return errors.Errorf("%s is not a valid region", region.Name)
		}

		for j, s := range region.Stacks {
			err := validate.Struct(s)
			if err != nil {
				for _, e := range err.(validator.ValidationErrors) {
					if e.Field() == "StackName" {
						return errors.Errorf("Stack name is missing for %d element", j)
					} else {
						return errors.Errorf("Missing field %s for stack %s in Region %s", e.Field(), s.StackName, region.Name)
					}
				}
			}
		}
	}

	return nil
}

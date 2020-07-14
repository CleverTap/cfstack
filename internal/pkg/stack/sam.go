package stack

import (
	"compress/flate"
	"fmt"
	"github.com/CleverTap/cfstack/internal/pkg/aws/s3"
	"github.com/CleverTap/cfstack/internal/pkg/util"
	"github.com/golang/glog"
	"github.com/google/uuid"
	"github.com/mholt/archiver"
	"github.com/wizrocket/goformation"
	"os"
	"path/filepath"
	"strconv"
)

func (s *Stack) packageServerlessTemplate() error {
	sourceBucket, err := s.Deployer.GetStackResourcePhysicalId("cfstack-Init", "SourceS3Bucket")

	if err != nil {
		return err
	}

	template, err := goformation.Open(s.AbsTemplatePath)

	if err != nil {
		return err
	}

	parsedTemplate, err := util.ParseJsonFile(s.AbsTemplatePath)

	if err != nil {
		return err
	}

	templateBasePath := filepath.Dir(s.AbsTemplatePath)
	templateFile := filepath.Base(s.AbsTemplatePath)

	functions := template.GetAllAWSServerlessFunctionResources()
	for name := range functions {

		j, err := strconv.Unquote(parsedTemplate.Path("Resources." + name + ".Properties.CodeUri").String())
		if err != nil {
			return err
		}

		functionCodePath := util.ResolvePath(templateBasePath, j)

		uid, err := uuid.NewUUID()
		if err != nil {
			return err
		}

		zipFilePath, err := prepare_zip_file(functionCodePath, uid.String())

		if err != nil {
			return err
		}

		uploadOpts := s3.Opts{
			Bucket:   sourceBucket,
			Filepath: zipFilePath,
			Key:      "lambda/" + uid.String(),
		}

		err = s.Uploader.UploadToS3(&uploadOpts)

		if err != nil {
			glog.Errorf("packaged template upload for stack %s failed", s.StackName)
			return err
		}

		_, err = parsedTemplate.SetP("s3://"+sourceBucket+"/lambda/"+uid.String(), "Resources."+name+".Properties.CodeUri")

		err = os.Remove(zipFilePath)
		if err != nil {
			return err
		}
	}

	// Template file
	s.AbsTemplatePath = filepath.Join(templateBasePath, s.Region+"-packaged-"+templateFile)
	f, err := os.Create(s.AbsTemplatePath)
	if err != nil {
		return err
	}

	defer f.Close()

	_, err = fmt.Fprintf(f, parsedTemplate.StringIndent("", "  "))

	return err
}

func prepare_zip_file(path string, uid string) (string, error) {
	var contents []string
	var basePath string

	isFile, err := util.IsFile(path)
	if err != nil {
		return "", err
	}

	if isFile {
		isZipFile, err := util.IsZipFile(path)
		if err != nil {
			return "", err
		}

		if isZipFile {
			return path, nil
		}

		contents = append(contents, path)
		basePath, err = filepath.Abs(filepath.Dir(path))
		if err != nil {
			return "", err
		}
	} else {
		contents, err = util.ListContents(path)
		if err != nil {
			return "", err
		}
		basePath = path
	}

	zipFilePath := filepath.Join(filepath.Dir(basePath), uid+".zip")

	z := archiver.Zip{
		CompressionLevel:       flate.DefaultCompression,
		MkdirAll:               true,
		SelectiveCompression:   true,
		ContinueOnError:        false,
		OverwriteExisting:      false,
		ImplicitTopLevelFolder: false,
	}
	err = z.Archive(contents, zipFilePath)
	if err != nil {
		return "", err
	}

	return zipFilePath, nil
}

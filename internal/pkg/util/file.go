package util

import (
	"archive/zip"
	"errors"
	"github.com/Jeffail/gabs"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

func WriteToFile(c []byte, path string) error {

	if !filepath.IsAbs(path) {
		dir, err := os.Getwd()
		if err != nil {
			return err
		}
		path = filepath.Join(dir, path)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(c)
	return err
}

func FileExists(path string) bool {
	_, err := os.Stat(path)

	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
		panic(err) // some other race condition, we panic!!
	}
	return true
}

func IsFile(path string) (bool, error) {
	fi, err := os.Stat(path)

	if err != nil {
		return false, err
	}

	switch mode := fi.Mode(); {
	case mode.IsDir():
		return false, nil
	case mode.IsRegular():
		return true, nil
	}

	return false, errors.New("could not check wether file or directory")
}

func IsZipFile(path string) (bool, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		if strings.Contains(err.Error(), "not a valid zip file") {
			return false, nil
		}

		return false, err
	}
	defer r.Close()

	return true, nil
}

func ParseJsonFile(file string) (*gabs.Container, error) {
	f, err := os.Open(file)
	// if we os.Open returns an error then handle it
	if err != nil {
		return nil, err
	}
	defer f.Close()

	byteValue, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}

	parsedTemplate, err := gabs.ParseJSON(byteValue)
	if err != nil {
		return nil, err
	}

	return parsedTemplate, nil
}

func ResolvePath(parent string, path string) string {
	if filepath.IsAbs(path) {
		return path
	}

	pathBase := filepath.Dir(path)

	if pathBase == ".." {
		return filepath.Join(filepath.Dir(parent), filepath.Base(path))
	}

	if pathBase == "." {
		return filepath.Join(parent, filepath.Base(path))
	}

	return filepath.Join(parent, path)
}

func ListContents(directory string) (filenames []string, err error) {
	contents, err := ioutil.ReadDir(directory)
	if err != nil {
		return nil, err
	}

	for _, file := range contents {
		filenames = append(filenames, filepath.Join(directory, file.Name()))
	}

	return filenames, nil
}

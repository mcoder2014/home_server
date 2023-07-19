package myfile

import (
	"fmt"
	"os"
	"path/filepath"
)

func ListDir(dirPath string) ([]string, []string, error) {
	if ok, _ := DirExist(dirPath); !ok {
		return nil, nil, fmt.Errorf("please check dir:%v existed and is dir", dirPath)
	}
	dir, err := os.Open(dirPath)
	if err != nil {
		return nil, nil, err
	}
	defer dir.Close()

	dirContents, err := dir.Readdir(-1)
	if err != nil {
		return nil, nil, err
	}
	var fileList, dirList []string
	for _, item := range dirContents {
		if !item.IsDir() {
			fileList = append(fileList, item.Name())
		} else {
			dirList = append(dirList, item.Name())
		}
	}

	return fileList, dirList, nil
}

func FileExist(filename string) (bool, error) {
	t, err := os.Stat(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return !t.IsDir(), nil
}

func DirExist(dir string) (bool, error) {
	t, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return t.IsDir(), nil
}

func Join(dir, file string) string {
	abs, _ := filepath.Abs(dir)
	return filepath.Join(abs, file)
}

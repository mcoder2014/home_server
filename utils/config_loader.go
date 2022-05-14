package utils

import (
	"gopkg.in/yaml.v3"
	"io/ioutil"
)

// BindConfig 将 yaml 配置文件绑定到 struct 上
func BindConfig(filepath string, config interface{}) error {
	//读取yaml文件到缓存中
	fileIO, err := ioutil.ReadFile(filepath)
	if err != nil {
		return err
	}

	//yaml文件内容影射到结构体中
	err = yaml.Unmarshal(fileIO,config)
	return err
}

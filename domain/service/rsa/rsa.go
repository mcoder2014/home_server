package rsa

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"

	myErrors "github.com/mcoder2014/home_server/errors"
)

func GenKey() (pubKey, prvKey []byte, err error) {
	// 生成私钥文件
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		return
	}
	derStream := x509.MarshalPKCS1PrivateKey(privateKey)
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: derStream,
	}
	prvKey = pem.EncodeToMemory(block)

	publicKey := &privateKey.PublicKey
	derPkix, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return
	}
	block = &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: derPkix,
	}
	pubKey = pem.EncodeToMemory(block)
	return
}

// Decrypt 私钥解密
func Decrypt(keyBytes, ciphertext []byte) ([]byte, error) {
	//获取私钥
	block, _ := pem.Decode(keyBytes)
	if block == nil {
		return nil, myErrors.New(myErrors.ErrorCodeGenRsaKeyFailed)
	}
	//解析 PKCS1 格式的私钥
	priv, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, myErrors.Wrapf(err, myErrors.ErrorCodeGenRsaKeyFailed, "ParsePKCS1PrivateKey failed")
	}
	// 解密
	data, err := rsa.DecryptPKCS1v15(rand.Reader, priv, ciphertext)
	if err != nil {
		return nil, myErrors.Wrapf(err, myErrors.ErrorCodeGenRsaKeyFailed, "rsa Decrypt failed")
	}
	return data, nil
}

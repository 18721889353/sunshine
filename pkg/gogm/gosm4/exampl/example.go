package main

import (
	"fmt"
	"github.com/18721889353/sunshine/pkg/gogm/gosm4"
)

func main() {
	key := []byte("1234567890123456")
	iv := []byte("1234567890123456")
	fmt.Println(string(iv), string(key))
	data := []byte("hello world")
	sm4 := gosm4.NewSM4(gosm4.WithUnescapeHTML(false))
	//enData, err := sm4.EncryptECB(data, key).ToHex()
	//if err != nil {
	//	panic(err)
	//}
	//fmt.Println(enData)
	//bytes, err := sm4.DecryptECBFromHex("a6c66e3894a0b213d4f204f78d6def09", key).ToBytes()
	//if err != nil {
	//	panic(err)
	//}
	//fmt.Println(string(bytes))

	enData, err := sm4.EncryptCBC(data, key, iv).ToHex()
	if err != nil {
		panic(err)
	}
	fmt.Println(enData)
	bytes, err := sm4.DecryptCBCFromHex("80e807ef81b5b9603bf02e9e495b0cbc", key, iv).ToBytes()
	if err != nil {
		panic(err)
	}
	fmt.Println(string(bytes))
}

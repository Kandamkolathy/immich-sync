package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
	"github.com/spf13/viper"
)

type ChecksumPair struct {
	Checksum string `json:"checksum"`
	ID       string `json:"id"`
}

type BulkCheckResponse struct {
	Results []struct {
		Action    string `json:"action"`
		AssetID   string `json:"assetId"`
		ID        string `json:"id"`
		IsTrashed bool   `json:"isTrashed"`
		Reason    string `json:"reason"`
	} `json:"results"`
}

type SupportedMediaTypesResponse struct {
	Image   []string `json:"image"`
	Sidecar []string `json:"sidecar"`
	Video   []string `json:"video"`
}

type ImmichClient struct {
	client http.Client
	server string
}

func NewImmichClient() ImmichClient {
	return ImmichClient{
		client: http.Client{},
		server: viper.GetString("server"),
	}
}

func (i *ImmichClient) UploadImage(path string) {
	url := i.server + "/api/assets"
	method := "POST"

	payload := &bytes.Buffer{}
	writer := multipart.NewWriter(payload)
	file, errFile1 := os.Open(path)
	defer file.Close()

	x, err := exif.Decode(file)
	if err != nil {
		log.Print(err)
	}

	file.Seek(0, 0)

	part1, errFile1 := writer.CreateFormFile("assetData", filepath.Base(path))
	_, errFile1 = io.Copy(part1, file)
	if errFile1 != nil {
		fmt.Println(errFile1)
		return
	}

	info, err := os.Stat(path)
	createdAt, err := x.DateTime()
	if err != nil {
		log.Print(err)
	}

	makeTag, err := x.Get(exif.Make)
	if err != nil {
		log.Print(err)
	}
	modelTag, err := x.Get(exif.Model)
	if err != nil {
		log.Print(err)
	}
	modelString, _ := modelTag.StringVal()
	makeString, _ := makeTag.StringVal()
	log.Print(modelString)
	_ = writer.WriteField("deviceAssetId", strings.TrimSuffix(filepath.Base(path), filepath.Ext(filepath.Base(path))))
	_ = writer.WriteField("deviceId", makeString+modelString)
	_ = writer.WriteField("fileCreatedAt", createdAt.Format(time.RFC3339))
	_ = writer.WriteField("fileModifiedAt", info.ModTime().Format(time.RFC3339))

	err = writer.Close()
	if err != nil {
		fmt.Println(err)
		return
	}

	req, err := http.NewRequest(method, url, payload)
	if err != nil {
		fmt.Println(err)
		return
	}
	req.Header.Add("Content-Type", "multipart/form-data")
	req.Header.Add("Accept", "application/json")
	req.Header.Add("x-api-key", viper.GetString("key"))

	req.Header.Set("Content-Type", writer.FormDataContentType())
	res, err := i.client.Do(req)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(string(body))

}

func (i *ImmichClient) GetNewFiles(shaMap []ChecksumPair) (BulkCheckResponse, error) {
	var data BulkCheckResponse

	url := i.server + "/api/assets/bulk-upload-check"
	method := "POST"

	payloadStruct := struct {
		Assets []ChecksumPair `json:"assets"`
	}{
		Assets: shaMap,
	}

	payload, err := json.Marshal(payloadStruct)
	if err != nil {
		return data, err
	}
	r := bytes.NewReader(payload)

	req, err := http.NewRequest(method, url, r)

	if err != nil {
		return data, err
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Accept", "application/json")
	req.Header.Add("x-api-key", viper.GetString("key"))

	res, err := i.client.Do(req)
	if err != nil {
		return data, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return data, err
	}

	json.Unmarshal(body, &data)

	return data, nil

}

func (i *ImmichClient) GetSupportedMediaTypes() (SupportedMediaTypesResponse, error) {
	url := i.server + "/api/server/media-types"
	method := "GET"

	data := SupportedMediaTypesResponse{}

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return data, err
	}

	res, err := i.client.Do(req)
	if err != nil {
		return data, err
	}

	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return data, err
	}

	err = json.Unmarshal(body, &data)
	if err != nil {
		return data, err
	}

	return data, nil
}

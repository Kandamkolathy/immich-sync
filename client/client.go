package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kardianos/service"
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
	client     http.Client
	server     string
	Logger     service.Logger
	MediaTypes SupportedMediaTypesResponse
}

var backoff []time.Duration = []time.Duration{500 * time.Millisecond, 5 * time.Second, 1 * time.Minute}

func NewImmichClient(logger service.Logger) (ImmichClient, error) {
	client := ImmichClient{
		client: http.Client{},
		server: viper.GetString("server"),
		Logger: logger,
	}

	err := client.CheckConnectivty()

	if err == nil {
		mediaTypes, _ := client.GetSupportedMediaTypes()
		client.MediaTypes = mediaTypes
		return client, nil
	}

	backoffIdx := 0
	ticker := time.NewTicker(backoff[backoffIdx])
	logger.Error("Server connectivty failed, retrying")
	for {
		select {
		case <-ticker.C:
			err := client.CheckConnectivty()
			if err == nil {
				mediaTypes, _ := client.GetSupportedMediaTypes()
				client.MediaTypes = mediaTypes
				return client, nil
			}
			logger.Error("Server connectivty failed, retrying")
			if backoffIdx < len(backoff)-1 {
				backoffIdx += 1
				ticker.Reset(backoff[backoffIdx])
			}
		}
	}

}

func (i *ImmichClient) CheckConnectivty() error {
	url := i.server + "/api/server/ping"
	method := "GET"

	i.Logger.Info("Checking connectivity")
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return err
	}

	res, err := i.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	if string(body) != "{\"res\":\"pong\"}" {
		return errors.New("Ping failed")
	}
	fmt.Println(string(body))

	i.Logger.Info("Connectivity exists")
	return nil
}

func (i *ImmichClient) WaitForConnectivity(connectedStatus chan<- bool) {
	backoffIdx := 0
	ticker := time.NewTicker(backoff[backoffIdx])
	for {
		select {
		case <-ticker.C:
			err := i.CheckConnectivty()
			if err == nil {
				connectedStatus <- true
				i.Logger.Info("Connectivty established")
				return
			}
			i.Logger.Info("Connectivty failed, trying again...")
			if backoffIdx < len(backoff)-1 {
				backoffIdx += 1
				ticker.Reset(backoff[backoffIdx])
			}
		}
	}
}

func (i *ImmichClient) UploadImage(path string) ([]byte, error) {
	url := i.server + "/api/assets"
	method := "POST"

	payload := &bytes.Buffer{}
	writer := multipart.NewWriter(payload)
	file, errFile1 := os.Open(path)
	defer file.Close()

	x, err := exif.Decode(file)
	if err != nil {
		return nil, err
	}

	file.Seek(0, 0)

	part1, errFile1 := writer.CreateFormFile("assetData", filepath.Base(path))
	_, errFile1 = io.Copy(part1, file)
	if errFile1 != nil {
		return nil, errFile1
	}

	info, err := os.Stat(path)
	createdAt, err := x.DateTime()
	if err != nil {
		return nil, err
	}

	makeTag, err := x.Get(exif.Make)
	if err != nil {
		return nil, err
	}
	modelTag, err := x.Get(exif.Model)
	if err != nil {
		return nil, err
	}
	modelString, _ := modelTag.StringVal()
	makeString, _ := makeTag.StringVal()

	_ = writer.WriteField("deviceAssetId", strings.TrimSuffix(filepath.Base(path), filepath.Ext(filepath.Base(path))))
	_ = writer.WriteField("deviceId", makeString+modelString)
	_ = writer.WriteField("fileCreatedAt", createdAt.Format(time.RFC3339))
	_ = writer.WriteField("fileModifiedAt", info.ModTime().Format(time.RFC3339))

	err = writer.Close()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(method, url, payload)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "multipart/form-data")
	req.Header.Add("Accept", "application/json")
	req.Header.Add("x-api-key", viper.GetString("key"))

	req.Header.Set("Content-Type", writer.FormDataContentType())
	res, err := i.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	return body, nil

}

func (i *ImmichClient) BulkUpload(buffer []string) error {
	i.Logger.Info("Uploading buffer")
	for _, file := range buffer {
		res, err := i.UploadImage(file)
		// Check for HTTP error
		if err != nil {
			connectivityErr := i.CheckConnectivty()
			if connectivityErr != nil {
				return connectivityErr
			}
			return err
		}
		i.Logger.Info(string(res))
		// Log uploaded files
	}
	return nil
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

	var data SupportedMediaTypesResponse

	defaultMediaTypes := "{\"video\":[\".3gp\",\".3gpp\",\".avi\",\".flv\",\".insv\",\".m2ts\",\".m4v\",\".mkv\",\".mov\",\".mp4\",\".mpe\",\".mpeg\",\".mpg\",\".mts\",\".webm\",\".wmv\"],\"image\":[\".3fr\",\".ari\",\".arw\",\".cap\",\".cin\",\".cr2\",\".cr3\",\".crw\",\".dcr\",\".dng\",\".erf\",\".fff\",\".iiq\",\".k25\",\".kdc\",\".mrw\",\".nef\",\".nrw\",\".orf\",\".ori\",\".pef\",\".psd\",\".raf\",\".raw\",\".rw2\",\".rwl\",\".sr2\",\".srf\",\".srw\",\".x3f\",\".avif\",\".bmp\",\".gif\",\".heic\",\".heif\",\".hif\",\".insp\",\".jpe\",\".jpeg\",\".jpg\",\".jxl\",\".png\",\".svg\",\".tif\",\".tiff\",\".webp\"],\"sidecar\":[\".xmp\"]}"
	json.Unmarshal([]byte(defaultMediaTypes), &data)

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

func (i *ImmichClient) IsExtensionSupported(ext string) bool {
	normalisedExt := strings.ToLower(ext)

	for _, t := range i.MediaTypes.Image {
		if t == normalisedExt {
			return true
		}
	}

	return false
}

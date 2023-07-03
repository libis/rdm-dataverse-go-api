// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"

	"github.com/libis/rdm-dataverse-go-api/api"
)

var server = "https://demo.dataverse.org"

// Token access
var apiToken = "your-api-token"

// URL signing
var user = "user-name"
var adminApiKey = "admin-api-key"
var unblockKey = "unblock-key"

func main() {
	dataset, err := GetDataset(PublicClient(), "doi:10.70122/FK2/UQ2GE8")
	if err != nil {
		fmt.Println("getting dataset failed:", err)
	} else {
		fmt.Println("dataset retrieved:", dataset.Status)
	}

	fileBytes, err := DownloadFileByIdAsBytes(PublicClient(), "doi:10.70122/FK2/UQ2GE8/0TAQZ8")
	if err != nil {
		fmt.Println("getting file failed:", err)
	} else {
		fmt.Println("file retrieved:", len(fileBytes))
	}

	addFileResp, err := AddFile(TokenAccessClient(), "doi:10.70122/FK2/UBHLMO")
	if err != nil {
		fmt.Println("replacing file failed:", err)
	} else {
		fmt.Println("file replaced:", addFileResp.Status)
	}

	addFileMapResp, err := AddFileUsingMapsAsJson(TokenAccessClient(), "doi:10.70122/FK2/UBHLMO")
	if err != nil {
		fmt.Println("replacing file using maps failed:", err)
	} else {
		fmt.Println("file replaced using maps:", addFileMapResp["status"])
	}
}

func PublicClient() *api.Client {
	return api.NewClient(server)
}

func TokenAccessClient() *api.Client {
	return api.NewTokenAccessClient(server, apiToken)
}

func UrlSigningClient() *api.Client {
	return api.NewUrlSigningClient(server, user, adminApiKey, unblockKey)
}

func GetDataset(client *api.Client, persistentId string) (res api.ListResponse, err error) {
	path := "/api/v1/datasets/:persistentId/versions/:latest/files?persistentId=" + persistentId
	req := client.NewRequest(path, "GET", nil, nil)
	err = api.Do(context.TODO(), req, &res)
	return
}

func DownloadFileById(client *api.Client, fileId string) (io.ReadCloser, error) {
	path := "/api/v1/access/datafile/:persistentId?persistentId=" + fileId
	req := client.NewRequest(path, "GET", nil, nil)
	return api.DoStream(context.TODO(), req)
}

func DownloadFileByIdAsBytes(client *api.Client, fileId string) ([]byte, error) {
	res, err := DownloadFileById(client, fileId)
	if err != nil {
		return nil, err
	}
	defer res.Close()
	return io.ReadAll(res)
}

func newAddFileRequest(client *api.Client, persistentId string, jsonData []byte) *api.Request {
	path := "/api/v1/datasets/:persistentId/add?persistentId=" + persistentId
	reader, contentType := readerFromFile(jsonData)
	requestHeader := http.Header{}
	requestHeader.Add("Content-Type", contentType)
	return client.NewRequest(path, "POST", reader, requestHeader)
}

func readerFromFile(jsonData []byte) (io.Reader, string) {
	w := bytes.NewBuffer(nil)
	multipartWriter := multipart.NewWriter(w)
	defer multipartWriter.Close()
	part1, _ := multipartWriter.CreateFormField("jsonData")
	part1.Write(jsonData)
	part2, _ := multipartWriter.CreateFormFile("file", "main.go")
	data, _ := os.ReadFile("examples/main.go")
	part2.Write(data)
	return w, multipartWriter.FormDataContentType()
}

func AddFile(client *api.Client, persistentId string) (res api.AddReplaceFileResponse, err error) {
	jsonData, _ := json.Marshal(api.JsonData{
		DirectoryLabel: "examples",
	})
	req := newAddFileRequest(client, persistentId, jsonData)
	err = api.Do(context.TODO(), req, &res)
	return
}

func AddFileUsingMapsAsJson(client *api.Client, persistentId string) (res map[string]interface{}, err error) {
	jsonData, _ := json.Marshal(map[string]interface{}{
		"directoryLabel": "examples",
	})
	req := newAddFileRequest(client, persistentId, jsonData)
	res = make(map[string]interface{})
	err = api.Do(context.TODO(), req, &res)
	return
}

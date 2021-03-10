// ------------------------------------------------------------
// Copyright (c) Microsoft Corporation and Dapr Contributors.
// Licensed under the MIT License.
// ------------------------------------------------------------

package localstorage

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Azure/azure-storage-blob-go/azblob"
	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/dapr/components-contrib/bindings"
	"github.com/dapr/dapr/pkg/logger"
	"github.com/google/uuid"
)

const (
	fileName = "fileName"
)

// LocalStorage allows saving files to disk
type LocalStorage struct {
	metadata     *Metadata
	containerURL azblob.ContainerURL

	logger logger.Logger
}

// Metadata defines the metadata
type Metadata struct {
	RootPath string `json:"rootPath"`
}

// NewLocalStorage returns a new LocalStorage instance
func NewLocalStorage(logger logger.Logger) *LocalStorage {
	return &LocalStorage{logger: logger}
}

// Init performs metadata parsing
func (ls *LocalStorage) Init(metadata bindings.Metadata) error {
	m, err := ls.parseMetadata(metadata)
	if err != nil {
		return err
	}
	ls.metadata = m

	err = os.MkdirAll(ls.metadata.RootPath, 0666)
	if err != nil {
		return fmt.Errorf("unable to create directory specified by 'rootPath': %s", ls.metadata.RootPath)
	}

	return nil
}

func (ls *LocalStorage) parseMetadata(metadata bindings.Metadata) (*Metadata, error) {
	lsInfo := metadata.Properties
	b, err := json.Marshal(lsInfo)
	if err != nil {
		return nil, err
	}

	var m Metadata
	err = json.Unmarshal(b, &m)
	if err != nil {
		return nil, err
	}

	return &m, nil
}

// Operations enumerates supported binding operations
func (ls *LocalStorage) Operations() []bindings.OperationKind {
	return []bindings.OperationKind{
		bindings.CreateOperation,
		bindings.GetOperation,
		bindings.ListOperation,
		bindings.DeleteOperation,
	}
}

func (ls *LocalStorage) create(filename string, req *bindings.InvokeRequest) (*bindings.InvokeResponse, error) {
	d, err := strconv.Unquote(string(req.Data))
	if err == nil {
		req.Data = []byte(d)
	}

	decoded, err := base64.StdEncoding.DecodeString(string(req.Data))
	if err == nil {
		req.Data = decoded
	}

	absPath, _, err := getSecureAbsRelPath(ls.metadata.RootPath, filename)
	if err != nil {
		return nil, err
	}

	err = os.MkdirAll(filepath.Dir(absPath), 0666)
	if err != nil {
		return nil, err
	}

	f, err := os.Create(absPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	numBytes, err := f.Write(req.Data)
	if err != nil {
		return nil, err
	}

	ls.logger.Debugf("wrote file: %s. numBytes: %d", absPath, numBytes)

	return nil, nil
}

func (ls *LocalStorage) get(filename string, req *bindings.InvokeRequest) (*bindings.InvokeResponse, error) {
	absPath, _, err := getSecureAbsRelPath(ls.metadata.RootPath, filename)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(absPath)
	if err != nil {
		ls.logger.Debugf("%s", err)
		return nil, err
	}

	b, err := io.ReadAll(f)
	if err != nil {
		ls.logger.Debugf("%s", err)
		return nil, err
	}

	ls.logger.Debugf("read file: %s. size: %d bytes", absPath, len(b))

	return &bindings.InvokeResponse{
		Data: b,
	}, nil
}

func (ls *LocalStorage) delete(filename string, req *bindings.InvokeRequest) (*bindings.InvokeResponse, error) {
	absPath, _, err := getSecureAbsRelPath(ls.metadata.RootPath, filename)
	if err != nil {
		return nil, err
	}

	err = os.Remove(absPath)
	if err != nil {
		return nil, err
	}

	ls.logger.Debugf("removed file: %s.", absPath)

	return nil, nil
}

func (ls *LocalStorage) list(filename string, req *bindings.InvokeRequest) (*bindings.InvokeResponse, error) {
	absPath, _, err := getSecureAbsRelPath(ls.metadata.RootPath, filename)
	if err != nil {
		return nil, err
	}

	fi, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}

	if !fi.IsDir() {
		return nil, errors.New(fmt.Sprintf("unable to list files as the file specified is not a directory [%s]", absPath))
	}

	files, err := walkPath(absPath)
	if err != nil {
		return nil, err
	}

	b, err := json.Marshal(files)
	if err != nil {
		return nil, err
	}

	return &bindings.InvokeResponse{
		Data: b,
	}, nil
}

func getSecureAbsRelPath(rootPath string, filename string) (absPath string, relPath string, err error) {
	absPath, err = securejoin.SecureJoin(rootPath, filename)
	relPath, err = filepath.Rel(rootPath, absPath)
	return
}

func walkPath(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// Invoke is called for output bindings
func (ls *LocalStorage) Invoke(req *bindings.InvokeRequest) (*bindings.InvokeResponse, error) {
	filename := ""
	if val, ok := req.Metadata[fileName]; ok && val != "" {
		filename = val
	} else {
		filename = uuid.New().String()
	}

	switch req.Operation {
	case bindings.CreateOperation:
		return ls.create(filename, req)
	case bindings.GetOperation:
		return ls.get(filename, req)
	case bindings.DeleteOperation:
		return ls.delete(filename, req)
	case bindings.ListOperation:
		return ls.list(filename, req)
	default:
		return nil, fmt.Errorf("unsupported operation %s", req.Operation)
	}
}

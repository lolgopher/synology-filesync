package synology

import (
	"github.com/lolgopher/synology-filesync/internal/app"
	"github.com/lolgopher/synology-filesync/protocol"
)

type Client struct {
	client *protocol.SynologyClient
}

var _ app.SynologyClient = (*Client)(nil)

func NewClient(info *protocol.ConnectionInfo) (*Client, error) {
	client, err := protocol.NewSynologyClient(info)
	if err != nil {
		return nil, err
	}

	return &Client{client: client}, nil
}

func (c *Client) GetFileList(folderPath string) (*protocol.FileListResponse, error) {
	return c.client.GetFileList(folderPath)
}

func (c *Client) DownloadFile(filePath, destPath string) (string, int64, error) {
	return c.client.DownloadFile(filePath, destPath)
}

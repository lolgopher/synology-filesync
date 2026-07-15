package sftp

import (
	"github.com/lolgopher/synology-filesync/internal/app"
	"github.com/lolgopher/synology-filesync/protocol"
)

type Client struct {
	client *protocol.SFTPClient
}

var _ app.SFTPClient = (*Client)(nil)

func NewClient(info *protocol.ConnectionInfo) (*Client, error) {
	client, err := protocol.NewSFTPClient(info)
	if err != nil {
		return nil, err
	}

	return &Client{client: client}, nil
}

func (c *Client) SendFile(localFilePath, remoteFilePath string) (int, error) {
	return c.client.SendFile(localFilePath, remoteFilePath)
}

func (c *Client) RemoveFile(targetFilePath string) error {
	return c.client.RemoveFile(targetFilePath)
}

func (c *Client) FreeSpace(path string) (uint64, error) {
	stat, err := c.client.Client.StatVFS(path)
	if err != nil {
		return 0, err
	}

	return stat.FreeSpace(), nil
}

func (c *Client) Close() error {
	return c.client.Close()
}

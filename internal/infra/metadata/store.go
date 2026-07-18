package metadata

import "github.com/lolgopher/synology-filesync/protocol"

type Store struct{}

func NewStore() *Store {
	return &Store{}
}

func (*Store) Read(folderPath, filename string) (map[string]protocol.FileMetadata, error) {
	return protocol.ReadMetadata(folderPath, filename)
}

func (*Store) Write(filePath, filename string, size uint64, status protocol.FileTransferStatus) error {
	return protocol.WriteMetadata(filePath, filename, size, status)
}

func (*Store) Exists(path string) bool {
	return protocol.FileExists(path)
}

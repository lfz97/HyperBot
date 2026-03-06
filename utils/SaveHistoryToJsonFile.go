package utils

import (
	"encoding/json"
	"os"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

func SaveHistoryToJsonFile(history []model.Message, path string) error {
	fd, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	data, err := json.Marshal(history)
	if err != nil {
		return err
	}
	fd.Write(data)
	return nil
}

package yt

import (
	"fmt"

	"github.com/go-faster/yt/ypath"
)

func WebUITableURL(cluster string, path ypath.Path) string {
	return fmt.Sprintf("https://yt.yandex-team.ru/%s/navigation?path=%s",
		cluster,
		string(path))
}

func WebUIOperationURL(cluster string, opID OperationID) string {
	return fmt.Sprintf("https://yt.yandex-team.ru/%s/operations/%s/details",
		cluster,
		opID.String())
}

// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"encoding/json"

	"github.com/golang/glog"
)

func logToolUse(c claudeContent) {
	var inp map[string]interface{}
	if err := json.Unmarshal(c.Input, &inp); err != nil {
		glog.V(2).Infof("claude: [%s]", c.Name)
		return
	}

	switch c.Name {
	case "Read":
		glog.V(2).Infof("claude: [read] %v", inp["file_path"])
	case "Write":
		glog.V(2).Infof("claude: [write] %v", inp["file_path"])
	case "Edit":
		glog.V(2).Infof("claude: [edit] %v", inp["file_path"])
	case "Grep":
		glog.V(2).Infof("claude: [grep] %v", inp["pattern"])
	case "Glob":
		glog.V(2).Infof("claude: [glob] %v", inp["pattern"])
	case "Bash":
		glog.V(2).Infof("claude: [bash] %v", inp["command"])
	default:
		glog.V(2).Infof("claude: [%s] %v", c.Name, inp)
	}
}

// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude

import (
	"encoding/json"

	"github.com/golang/glog"
)

func logToolUse(c claudeContent) {
	if !bool(glog.V(2)) {
		return
	}
	var inp map[string]any
	if err := json.Unmarshal(c.Input, &inp); err != nil {
		glog.Infof("claude: [%s]", c.Name)
		return
	}

	switch c.Name {
	case "Read":
		glog.Infof("claude: [read] %v", inp["file_path"])
	case "Write":
		glog.Infof("claude: [write] %v", inp["file_path"])
	case "Edit":
		glog.Infof("claude: [edit] %v", inp["file_path"])
	case "Grep":
		glog.Infof("claude: [grep] %v", inp["pattern"])
	case "Glob":
		glog.Infof("claude: [glob] %v", inp["pattern"])
	case "Bash":
		glog.Infof("claude: [bash] %v", inp["command"])
	default:
		glog.Infof("claude: [%s] %v", c.Name, inp)
	}
}

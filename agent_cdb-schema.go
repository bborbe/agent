// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package lib

import "github.com/bborbe/cqrs/cdb"

var CDBSchemaIDs = cdb.SchemaIDs{
	TaskV1SchemaID,
}

var TaskV1SchemaID = cdb.SchemaID{
	Group:   "agent",
	Kind:    "task",
	Version: "v1",
}

package bridge

import "github.com/insightlabs38-pixel/vsf/backend/schema"

func knownDigest() string { return schema.KnownVersions[0][1] }

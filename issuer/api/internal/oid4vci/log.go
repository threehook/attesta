package oid4vci

import "log/slog"

func logError(err error) { slog.Error("oid4vci", "error", err) }

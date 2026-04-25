// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"

	"go.thesmos.sh/protoc-gen-codec/internal/core"
)

// Run executes the protoc-gen-codec-go plugin over the current protoc input
// stream. It is intended to be invoked from main.
func Run() {
	protogen.Options{}.Run(func(plugin *protogen.Plugin) error {
		plugin.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)
		fileMap := buildFileMap(plugin)
		var firstErr error
		for _, f := range plugin.Files {
			if !f.Generate {
				continue
			}
			if err := GenerateFile(plugin, f, fileMap); err != nil {
				plugin.Error(fmt.Errorf("%s: %w", f.Desc.Path(), err))
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		return firstErr
	})
}

func buildFileMap(plugin *protogen.Plugin) map[string]*protogen.File {
	m := make(map[string]*protogen.File, len(plugin.Files))
	for _, f := range plugin.Files {
		m[f.Proto.GetName()] = f
	}
	return m
}

// GenerateFile emits the codec methods for every annotated message in file.
// Exported so integration and compile tests can invoke it directly without
// driving the protoc plugin entry point.
func GenerateFile(
	plugin *protogen.Plugin,
	file *protogen.File,
	fileMap map[string]*protogen.File,
) error {
	aliasOf := func(dep *protogen.File) string { return string(dep.GoPackageName) }

	var messages []*core.MessageInfo
	for _, msg := range file.Messages {
		info, err := core.AnalyzeMessage(msg, fileMap, file, aliasOf)
		if err != nil {
			return err
		}
		if info == nil {
			continue // message not annotated for codec
		}
		messages = append(messages, info)
	}

	if len(messages) == 0 {
		return nil
	}

	return emitGoFile(plugin, file, fileMap, messages)
}

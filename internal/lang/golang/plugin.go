// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"google.golang.org/protobuf/compiler/protogen"

	"go.stealthscale.io/protoc-gen-codec/internal/core"
)

func Run() {
	protogen.Options{}.Run(func(plugin *protogen.Plugin) error {
		fileMap := buildFileMap(plugin)
		for _, f := range plugin.Files {
			if !f.Generate {
				continue
			}
			if err := generateFile(plugin, f, fileMap); err != nil {
				return err
			}
		}
		return nil
	})
}

func buildFileMap(plugin *protogen.Plugin) map[string]*protogen.File {
	m := make(map[string]*protogen.File, len(plugin.Files))
	for _, f := range plugin.Files {
		m[f.Proto.GetName()] = f
	}
	return m
}

func generateFile(
	plugin *protogen.Plugin,
	file *protogen.File,
	fileMap map[string]*protogen.File,
) error {
	return GenerateFile(plugin, file, fileMap)
}

// GenerateFile is exported for testing.
func GenerateFile(
	plugin *protogen.Plugin,
	file *protogen.File,
	fileMap map[string]*protogen.File,
) error {
	return generateFileImpl(plugin, file, fileMap)
}

func generateFileImpl(
	plugin *protogen.Plugin,
	file *protogen.File,
	fileMap map[string]*protogen.File,
) error {
	var messages []*core.MessageInfo
	for _, msg := range file.Messages {
		info, err := core.AnalyzeMessage(msg, fileMap, file)
		if err != nil {
			return err
		}
		if info == nil {
			continue
		}
		messages = append(messages, info)
	}

	if len(messages) == 0 {
		return nil
	}

	return emitGoFile(plugin, file, messages)
}

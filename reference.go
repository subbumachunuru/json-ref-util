package jsonrefutil

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

type OutputDirectoryData struct {
	outputDirectoryPath *string
}

type OutputDirectoryDataOption func(field OutputDirectoryData)

func WithOutputDirectoryPath(outputDirectoryPath string) OutputDirectoryDataOption {
	return func(r OutputDirectoryData) {
		*r.outputDirectoryPath = outputDirectoryPath
	}
}

func FetchDereferencedJson(filePath string) ([]byte, error) {

	jsonMap := make(map[string]interface{})

	if err := parseJsonFile(filePath, jsonMap); err != nil {
		return nil, err
	}

	referencedFiles := make([]string, 0)
	referencedFiles = append(referencedFiles, filePath)

	if err := resolveReferences(jsonMap, filepath.Dir(filePath), referencedFiles); err != nil {
		return nil, err
	}

	return json.Marshal(jsonMap)
}

func GenerateDereferencedJson(filePath string, options ...OutputDirectoryDataOption) error {

	outputDirectoryData := OutputDirectoryData{
		outputDirectoryPath: new(string),
	}

	for _, option := range options {
		option(outputDirectoryData)
	}

	jsonMap := make(map[string]interface{})

	if err := parseJsonFile(filePath, jsonMap); err != nil {
		return err
	}

	referencedFiles := make([]string, 0)
	referencedFiles = append(referencedFiles, filePath)

	if err := resolveReferences(jsonMap, filepath.Dir(filePath), referencedFiles); err != nil {
		return err
	}

	var outputDirectoryPath string
	if *outputDirectoryData.outputDirectoryPath != "" {
		outputDirectoryPath = *outputDirectoryData.outputDirectoryPath
	} else {
		outputDirectoryPath = filepath.Dir(filePath)
	}

	return generateUpdatedJson(filepath.Base(filePath), outputDirectoryPath, jsonMap)
}

func parseJsonFile(filePath string, jsonMap interface{}) error {
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	return json.Unmarshal(fileContent, jsonMap)
}

// resolveReferences function resolves references to other JSON files
func resolveReferences(data interface{}, basePath string, referencedFiles []string) error {
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid json data, jsonData = %v", data)
	}

	// Check for references
	if refData, exists := dataMap["$ref"]; exists {
		refValue, ok := refData.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid reference data, refData = %v", refData)
		}

		// File and key reference
		refPath, pathExists := refValue["path"].(string)
		refKey, keyExists := refValue["key"].(string)

		if !pathExists {
			return fmt.Errorf("$ref must have 'path' value, refvalue = %v", refValue)
		}

		refFilePath := filepath.Join(basePath, refPath)
		if alreadyReferenced := slices.Contains(referencedFiles, refFilePath); alreadyReferenced {
			return fmt.Errorf("cyclic reference detected, referencedFiles = %v, duplicateFilePath = %s", referencedFiles, refFilePath)
		} else {
			referencedFiles = append(referencedFiles, refFilePath)
		}

		refFileData := make(map[string]interface{})
		var referencedValue interface{}

		if err := parseJsonFile(refFilePath, &refFileData); err != nil {
			return err
		}

		if keyExists {
			var found bool
			// Extract the specific key from the referenced file
			referencedValue, found = refFileData[refKey]
			if !found {
				return fmt.Errorf("invalid referencing, referenced key %s not found in file %s", refKey, refPath)
			}
		} else {
			referencedValue = refFileData
		}

		// Recursively resolve references in the referenced file
		if err := resolveReferences(referencedValue, filepath.Dir(refFilePath), referencedFiles); err != nil {
			return err
		}

		// Replace the $ref with the referenced value
		delete(dataMap, "$ref")
		referencedData, ok := referencedValue.(map[string]interface{})
		if !ok {
			return fmt.Errorf("json format of referenced value is invalid, referencedValue = %v", referencedValue)
		}

		for key, value := range referencedData {
			dataMap[key] = value
		}
	}

	// Process other keys in the main data
	for _, value := range dataMap {
		if nestedMap, ok := value.(map[string]interface{}); ok {
			// Recursively resolve references in nested maps
			if err := resolveReferences(nestedMap, basePath, referencedFiles); err != nil {
				return err
			}
		}
	}

	// Check for overrides
	if overrideData, exists := dataMap["$override"]; exists {
		if err := override(dataMap, overrideData); err != nil {
			return err
		}

		// Remove the $override key from the main data
		delete(dataMap, "$override")
	}

	// Check for additions
	if addData, exists := dataMap["$add"]; exists {
		if err := add(dataMap, addData); err != nil {
			return err
		}

		// Remove the $add key from the main data
		delete(dataMap, "$add")
	}

	// Check for deletions
	if deleteData, exists := dataMap["$delete"]; exists {
		if err := remove(dataMap, deleteData); err != nil {
			return err
		}

		// Remove the $delet key from the main data
		delete(dataMap, "$delete")
	}

	return nil
}

func override(data map[string]interface{}, overrideData interface{}) error {
	overrideDataMap, ok := overrideData.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid $override value, overrideData = %v", overrideData)
	}

	// Apply overrides
	for key, value := range overrideDataMap {
		dataMap, ok1 := data[key].(map[string]interface{})
		nestedMap, ok2 := value.(map[string]interface{})
		if ok1 && ok2 {
			// Recursively apply overrides in nested maps
			if err := override(dataMap, nestedMap); err != nil {
				return err
			}
		} else if _, ok := data[key]; ok {
			data[key] = value
		}
	}

	return nil
}

func add(data map[string]interface{}, addData interface{}) error {
	addDataMap, ok := addData.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid $add value, addData = %v", addData)
	}

	// Apply additions
	for key, value := range addDataMap {
		dataMap, ok1 := data[key].(map[string]interface{})
		nestedMap, ok2 := value.(map[string]interface{})
		if ok1 && ok2 {
			// Recursively apply additions in nested maps
			if err := add(dataMap, nestedMap); err != nil {
				return err
			}
		} else if _, ok := data[key]; !ok {
			data[key] = value
		}
	}

	return nil
}

func remove(data map[string]interface{}, deleteData interface{}) error {
	deleteDataMap, ok := deleteData.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid $delete value, deleteData = %v", deleteData)
	}

	// Apply deletions
	for key, value := range deleteDataMap {
		dataMap, ok1 := data[key].(map[string]interface{})
		nestedMap, ok2 := value.(map[string]interface{})
		if ok1 && ok2 {
			// Recursively apply deletions in nested maps
			if err := remove(dataMap, nestedMap); err != nil {
				return err
			}
		} else if ok1 && !ok2 {
			deletableKeys, ok := value.([]interface{})
			if ok {
				// Delete the specified keys
				for _, deletableKey := range deletableKeys {
					delete(dataMap, deletableKey.(string))
				}
			}
		}
	}

	return nil
}

func generateUpdatedJson(fileName, outputPath string, data interface{}) error {
	updatedJson, err := json.MarshalIndent(data, "", "    ")
	if err != nil {
		return err
	}

	outputFilePath := filepath.Join(outputPath, "output_", fileName)
	if err = os.WriteFile(outputFilePath, updatedJson, 0644); err != nil {
		return fmt.Errorf("error writing updated json to file %s with error %v", outputFilePath, err)
	}

	return nil
}

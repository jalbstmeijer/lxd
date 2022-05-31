//go:build linux && cgo && !agent

package cluster

// The code below was generated by lxd-generate - DO NOT EDIT!

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lxc/lxd/lxd/db/query"
	"github.com/lxc/lxd/shared/api"
)

var _ = api.ServerEnvironment{}

const deviceObjects = `SELECT %s_devices.id, %s_devices.%s_id, %s_devices.name, %s_devices.type
  FROM %s_devices
  ORDER BY %s_devices.name`

const deviceCreate = `INSERT INTO %s_devices (%s_id, name, type)
  VALUES (?, ?, ?)`

const deviceDelete = `DELETE FROM %s_devices WHERE %s_id = ?`

// GetDevices returns all available devices for the parent entity.
// generator: device GetMany
func GetDevices(ctx context.Context, tx *sql.Tx, parent string) (map[int][]Device, error) {
	var err error

	// Result slice.
	objects := make([]Device, 0)

	deviceObjectsLocal := strings.Replace(deviceObjects, "%s_id", fmt.Sprintf("%s_id", parent), -1)
	fillParent := make([]any, strings.Count(deviceObjectsLocal, "%s"))
	for i := range fillParent {
		fillParent[i] = strings.Replace(parent, "_", "s_", -1) + "s"
	}

	sqlStmt, err := prepare(tx, fmt.Sprintf(deviceObjectsLocal, fillParent...))
	if err != nil {
		return nil, err
	}

	args := []any{}

	// Dest function for scanning a row.
	dest := func(i int) []any {
		objects = append(objects, Device{})
		return []any{
			&objects[i].ID,
			&objects[i].ReferenceID,
			&objects[i].Name,
			&objects[i].Type,
		}
	}

	// Select.
	err = query.SelectObjects(sqlStmt, dest, args...)
	if err != nil {
		return nil, fmt.Errorf("Failed to fetch from \"devices\" table: %w", err)
	}

	config, err := GetConfig(ctx, tx, parent+"_device")
	if err != nil {
		return nil, err
	}

	for i := range objects {
		if _, ok := config[objects[i].ID]; !ok {
			objects[i].Config = map[string]string{}
		} else {
			objects[i].Config = config[objects[i].ID]
		}
	}

	resultMap := map[int][]Device{}
	for _, object := range objects {
		if _, ok := resultMap[object.ReferenceID]; !ok {
			resultMap[object.ReferenceID] = []Device{}
		}
		resultMap[object.ReferenceID] = append(resultMap[object.ReferenceID], object)
	}

	return resultMap, nil
}

// CreateDevices adds a new device to the database.
// generator: device Create
func CreateDevices(ctx context.Context, tx *sql.Tx, parent string, objects map[string]Device) error {
	deviceCreateLocal := strings.Replace(deviceCreate, "%s_id", fmt.Sprintf("%s_id", parent), -1)
	fillParent := make([]any, strings.Count(deviceCreateLocal, "%s"))
	for i := range fillParent {
		fillParent[i] = strings.Replace(parent, "_", "s_", -1) + "s"
	}

	stmt, err := prepare(tx, fmt.Sprintf(deviceCreateLocal, fillParent...))
	if err != nil {
		return err
	}

	for _, object := range objects {
		result, err := stmt.Exec(object.ReferenceID, object.Name, object.Type)
		if err != nil {
			return fmt.Errorf("Insert failed for \"%s_devices\" table: %w", parent, err)
		}

		id, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("Failed to fetch ID: %w", err)
		}

		referenceID := int(id)
		for key, value := range object.Config {
			insert := Config{
				ReferenceID: referenceID,
				Key:         key,
				Value:       value,
			}

			err = CreateConfig(ctx, tx, parent+"_device", insert)
			if err != nil {
				return fmt.Errorf("Insert Config failed for Device: %w", err)
			}
		}
	}

	return nil
}

// UpdateDevices updates the device matching the given key parameters.
// generator: device Update
func UpdateDevices(ctx context.Context, tx *sql.Tx, parent string, referenceID int, devices map[string]Device) error {
	// Delete current entry.
	err := DeleteDevices(ctx, tx, parent, referenceID)
	if err != nil {
		return err
	}

	// Insert new entries.
	for key, object := range devices {
		object.ReferenceID = referenceID
		devices[key] = object
	}

	err = CreateDevices(ctx, tx, parent, devices)
	if err != nil {
		return err
	}

	return nil
}

// DeleteDevices deletes the device matching the given key parameters.
// generator: device DeleteMany
func DeleteDevices(ctx context.Context, tx *sql.Tx, parent string, referenceID int) error {
	deviceDeleteLocal := strings.Replace(deviceDelete, "%s_id", fmt.Sprintf("%s_id", parent), -1)
	fillParent := make([]any, strings.Count(deviceDeleteLocal, "%s"))
	for i := range fillParent {
		fillParent[i] = strings.Replace(parent, "_", "s_", -1) + "s"
	}

	stmt, err := prepare(tx, fmt.Sprintf(deviceDeleteLocal, fillParent...))
	if err != nil {
		return err
	}

	result, err := stmt.Exec(referenceID)
	if err != nil {
		return fmt.Errorf("Delete entry for \"%s_device\" failed: %w", parent, err)
	}

	_, err = result.RowsAffected()
	if err != nil {
		return fmt.Errorf("Fetch affected rows: %w", err)
	}

	return nil
}

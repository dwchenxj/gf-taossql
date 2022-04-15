package taossql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"github.com/gogf/gf/v2/container/gmap"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/text/gregex"
	"github.com/gogf/gf/v2/text/gstr"
)

// Driver is the driver for taossql database.
type Driver struct {
	*gdb.Core
}

var (
	// tableFieldsMap caches the table information retrieved from database.
	tableFieldsMap = gmap.New(true)
)

func init() {
	if err := gdb.Register(`taossql`, New()); err != nil {
		panic(err)
	}
}

// New create and returns a driver that implements gdb.Driver, which supports operations for taossql.
func New() gdb.Driver {
	return &Driver{}
}

// New creates and returns a database object for postgresql.
// It implements the interface of gdb.Driver for extra database driver installation.
func (d *Driver) New(core *gdb.Core, node *gdb.ConfigNode) (gdb.DB, error) {
	return &Driver{
		Core: core,
	}, nil
}

// Open creates and returns an underlying sql.DB object for taossql.
func (d *Driver) Open(config *gdb.ConfigNode) (db *sql.DB, err error) {
	var (
		source               string
		underlyingDriverName = "taossql"
	)
	if config.Link != "" {
		source = config.Link
	} else {
		source = fmt.Sprintf(
			"%s:%s/tcp(%s:%s)/%s",
			config.User, config.Pass, config.Host, config.Port, config.Name,
		)
		if config.Timezone != "" {
			source = fmt.Sprintf("%s timezone=%s", source, config.Timezone)
		}
	}
	if db, err = sql.Open(underlyingDriverName, source); err != nil {
		err = gerror.WrapCodef(
			gcode.CodeDbOperationError, err,
			`sql.Open failed for driver "%s" by source "%s"`, underlyingDriverName, source,
		)
		return nil, err
	}
	return
}

// FilteredLink retrieves and returns filtered `linkInfo` that can be using for
// logging or tracing purpose.
func (d *Driver) FilteredLink() string {
	linkInfo := d.GetConfig().Link
	if linkInfo == "" {
		return ""
	}
	s, _ := gregex.ReplaceString(
		`(.+?)\s*password=(.+)\s*host=(.+)`,
		`$1 password=xxx host=$3`,
		linkInfo,
	)
	return s
}

// GetChars returns the security char for this type of database.
func (d *Driver) GetChars() (charLeft string, charRight string) {
	return "\"", "\""
}

// DoFilter deals with the sql string before commits it to underlying sql driver.
func (d *Driver) DoFilter(ctx context.Context, link gdb.Link, sql string, args []interface{}) (newSql string, newArgs []interface{}, err error) {
	defer func() {
		newSql, newArgs, err = d.Core.DoFilter(ctx, link, newSql, newArgs)
	}()
	var index int
	// Convert placeholder char '?' to string "$x".
	sql, _ = gregex.ReplaceStringFunc(`\?`, sql, func(s string) string {
		index++
		return fmt.Sprintf(`$%d`, index)
	})
	sql, _ = gregex.ReplaceStringFuncMatch(`(::jsonb([^\w\d]*)\$\d)`, sql, func(match []string) string {
		return fmt.Sprintf(`::jsonb%s?`, match[2])
	})
	newSql, _ = gregex.ReplaceString(` LIMIT (\d+),\s*(\d+)`, ` LIMIT $2 OFFSET $1`, sql)
	return newSql, args, nil
}

// Tables retrieves and returns the tables of current schema.
// It's mainly used in cli tool chain for automatically generating the models.
func (d *Driver) Tables(ctx context.Context, schema ...string) (tables []string, err error) {
	var result gdb.Result
	link, err := d.SlaveLink(schema...)
	if err != nil {
		return nil, err
	}
	query := ""
	if len(schema) > 0 && schema[0] != "" {
		query = fmt.Sprintf(
			"desc %s",
			schema[0],
		)
	}
	result, err = d.DoSelect(ctx, link, query)
	if err != nil {
		return
	}
	for _, m := range result {
		for _, v := range m {
			tables = append(tables, v.String())
		}
	}
	return
}

// TableFields retrieves and returns the fields' information of specified table of current schema.
//
// Also see DriverMysql.TableFields.
func (d *Driver) TableFields(ctx context.Context, table string, schema ...string) (fields map[string]*gdb.TableField, err error) {
	charL, charR := d.GetChars()
	table = gstr.Trim(table, charL+charR)
	if gstr.Contains(table, " ") {
		return nil, gerror.NewCode(
			gcode.CodeInvalidParameter,
			"function TableFields supports only single table operations",
		)
	}
	table, _ = gregex.ReplaceString("\"", "", table)
	useSchema := d.GetSchema()
	if len(schema) > 0 && schema[0] != "" {
		useSchema = schema[0]
	}
	v := tableFieldsMap.GetOrSetFuncLock(
		fmt.Sprintf(`taossql_table_fields_%s_%s@group:%s`, table, useSchema, d.GetGroup()),
		func() interface{} {
			var (
				result       gdb.Result
				link         gdb.Link
				structureSql = fmt.Sprintf(`desc %s`, table)
			)
			if link, err = d.SlaveLink(useSchema); err != nil {
				return nil
			}
			structureSql, _ = gregex.ReplaceString(`[\n\r\s]+`, " ", gstr.Trim(structureSql))
			result, err = d.DoSelect(ctx, link, structureSql)
			if err != nil {
				return nil
			}
			fields = make(map[string]*gdb.TableField)
			for i, m := range result {
				fields[m["field"].String()] = &gdb.TableField{
					Index: i,
					Name:  m["field"].String(),
					Type:  m["type"].String(),
				}
			}
			return fields
		},
	)
	if v != nil {
		fields = v.(map[string]*gdb.TableField)
	}
	return
}

// DoInsert is not supported in taossql.
func (d *Driver) DoInsert(ctx context.Context, link gdb.Link, table string, list gdb.List, option gdb.DoInsertOption) (result sql.Result, err error) {
	switch option.InsertOption {
	case gdb.InsertOptionSave:
		return nil, gerror.NewCode(
			gcode.CodeNotSupported,
			`Save operation is not supported by taossql driver`,
		)

	case gdb.InsertOptionReplace:
		return nil, gerror.NewCode(
			gcode.CodeNotSupported,
			`Replace operation is not supported by taossql driver`,
		)

	default:
		return d.Core.DoInsert(ctx, link, table, list, option)
	}
}

// ConvertDataForRecord converting for any data that will be inserted into table/collection as a record.
func (d *Driver) ConvertDataForRecord(ctx context.Context, value interface{}) map[string]interface{} {
	data := gdb.DataToMapDeep(value)
	var err error
	for k, v := range data {
		if valuer, ok := v.(driver.Valuer); ok {
			data[k], err = valuer.Value()
			if err != nil {
				return nil
			}
		} else {
			data[k] = d.Core.ConvertDataForRecordValue(ctx, v)
		}
	}
	return data
}

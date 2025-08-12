package dao

import (
	"context"
	"errors"
	"fmt"
	"time"
    "github.com/18721889353/sunshine/pkg/gocrypto"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"github.com/18721889353/sunshine/pkg/logger"
	"github.com/18721889353/sunshine/pkg/sgorm/query"
	"github.com/18721889353/sunshine/pkg/utils"

	"github.com/18721889353/sunshine/internal/cache"
	"github.com/18721889353/sunshine/internal/database"
	"github.com/18721889353/sunshine/internal/model"

)

var _ {{.TableNameCamel}}Dao = (*{{.TableNameCamelFCL}}Dao)(nil)

// {{.TableNameCamel}}Dao defining the dao interface
type {{.TableNameCamel}}Dao interface {
	Create(ctx context.Context, table *model.{{.TableNameCamel}}) error
	CreateInBatches(ctx context.Context, tables []*model.{{.TableNameCamel}}, batchSize int) error
	DeleteBy{{.ColumnNameCamel}}(ctx context.Context, {{.ColumnNameCamelFCL}} {{.GoType}}) error
	UpdateBy{{.ColumnNameCamel}}(ctx context.Context, table *model.{{.TableNameCamel}}) error
	GetBy{{.ColumnNameCamel}}(ctx context.Context, {{.ColumnNameCamelFCL}} {{.GoType}}) (*model.{{.TableNameCamel}}, error)
	GetByColumns(ctx context.Context, params *query.Params) ([]*model.{{.TableNameCamel}}, int64, error)
	GetOneByColumns(ctx context.Context, params *query.Params) (*model.{{.TableNameCamel}}, error)

	DeleteBy{{.ColumnNamePluralCamel}}(ctx context.Context, {{.ColumnNamePluralCamelFCL}} []{{.GoType}}) error
	DeleteByCondition(ctx context.Context, c *query.Conditions) error

	GetByCondition(ctx context.Context, condition *query.Conditions) (ids []uint64, err error)
	GetBy{{.ColumnNamePluralCamel}}(ctx context.Context, {{.ColumnNamePluralCamelFCL}} []{{.GoType}}) (map[{{.GoType}}]*model.{{.TableNameCamel}}, error)
	GetByLast{{.ColumnNameCamel}}(ctx context.Context, last{{.ColumnNameCamel}} {{.GoType}}, limit int, sort string) ([]*model.{{.TableNameCamel}}, error)

	CreateByTx(ctx context.Context, tx *gorm.DB, table *model.{{.TableNameCamel}}) ({{.GoType}}, error)
	CreateByTxInBatches(ctx context.Context, tx *gorm.DB, tables []*model.{{.TableNameCamel}}, batchSize int) error
	DeleteByTx(ctx context.Context, tx *gorm.DB, {{.ColumnNameCamelFCL}} {{.GoType}}) error
	DeleteByTxCondition(ctx context.Context, tx *gorm.DB, c *query.Conditions) error

	UpdateByTx(ctx context.Context, tx *gorm.DB, table *model.{{.TableNameCamel}}) error
}

type {{.TableNameCamelFCL}}Dao struct {
	db    *gorm.DB
	cache cache.{{.TableNameCamel}}Cache // if nil, the cache is not used.
	sfg   *singleflight.Group    // if cache is nil, the sfg is not used.
}

// New{{.TableNameCamel}}Dao creating the dao interface
func New{{.TableNameCamel}}Dao(db *gorm.DB, xCache cache.{{.TableNameCamel}}Cache) {{.TableNameCamel}}Dao {
	if xCache == nil {
		return &{{.TableNameCamelFCL}}Dao{db: db,sfg: new(singleflight.Group)}
	}
	return &{{.TableNameCamelFCL}}Dao{
		db:    db,
		cache: xCache,
		sfg:   new(singleflight.Group),
	}
}

func (d *{{.TableNameCamelFCL}}Dao) deleteCache(ctx context.Context, {{.ColumnNameCamelFCL}} {{.GoType}}) error {
	if d.cache != nil {
		if id == 0 || id == 88888888 {
			defer func() {
				if id == 88888888 {
					_ = d.cache.DelByPrefix(ctx, cache.{{.TableNameCamelFCL}}CachePrefixKey)
				} else {
					_ = d.cache.DelByPrefix(ctx, cache.{{.TableNameCamelFCL}}CachePrefixKey+"condition:")
				}
			}()
		}
		return d.cache.Del(ctx, {{.ColumnNameCamelFCL}})
	}
	return nil
}

// Create a record, insert the record and the {{.ColumnNameCamelFCL}} value is written back to the table
func (d *{{.TableNameCamelFCL}}Dao) Create(ctx context.Context, table *model.{{.TableNameCamel}}) error {
	defer func() {
		_ = d.deleteCache(ctx, 0)
	}()
	return d.db.WithContext(ctx).Create(table).Error
}

func (d *{{.TableNameCamelFCL}}Dao) CreateInBatches(ctx context.Context, tables []*model.{{.TableNameCamel}}, batchSize int) error {
	defer func() {
		_ = d.deleteCache(ctx, 0)
	}()
	return d.db.WithContext(ctx).CreateInBatches(tables, batchSize).Error
}


// DeleteBy{{.ColumnNameCamel}} delete a record by {{.ColumnNameCamelFCL}}
func (d *{{.TableNameCamelFCL}}Dao) DeleteBy{{.ColumnNameCamel}}(ctx context.Context, {{.ColumnNameCamelFCL}} {{.GoType}}) error {
	defer func() {
		_ = d.deleteCache(ctx, 0)
	}()
	err := d.db.WithContext(ctx).Where("{{.ColumnName}} = ?", {{.ColumnNameCamelFCL}}).Delete(&model.{{.TableNameCamel}}{}).Error
	if err != nil {
		return err
	}

	// delete cache
	_ = d.deleteCache(ctx, {{.ColumnNameCamelFCL}})

	return nil
}

// UpdateBy{{.ColumnNameCamel}} update a record by {{.ColumnNameCamelFCL}}
func (d *{{.TableNameCamelFCL}}Dao) UpdateBy{{.ColumnNameCamel}}(ctx context.Context, table *model.{{.TableNameCamel}}) error {
	err := d.updateDataBy{{.ColumnNameCamel}}(ctx, d.db, table)

	// delete cache
	_ = d.deleteCache(ctx, table.{{.ColumnNameCamel}})

	return err
}

func (d *{{.TableNameCamelFCL}}Dao) updateDataBy{{.ColumnNameCamel}}(ctx context.Context, db *gorm.DB, table *model.{{.TableNameCamel}}) error {
	{{if .IsStringType}}if table.{{.ColumnNameCamel}} == "" {
		return errors.New("{{.ColumnNameCamelFCL}} cannot be empty")
	}
{{else}}	if table.{{.ColumnNameCamel}} < 1 {
		return errors.New("{{.ColumnNameCamelFCL}} cannot be 0")
	}
{{end}}

	update := map[string]interface{}{}
	// todo generate the update fields code to here
	// delete the templates code start
	if table.Name != "" {
		update["name"] = table.Name
	}
	if table.Password != "" {
		update["password"] = table.Password
	}
	if table.Email != "" {
		update["email"] = table.Email
	}
	if table.Phone != "" {
		update["phone"] = table.Phone
	}
	if table.Avatar != "" {
		update["avatar"] = table.Avatar
	}
	if table.Age > 0 {
		update["age"] = table.Age
	}
	if table.Gender > 0 {
		update["gender"] = table.Gender
	}
	if table.LoginAt > 0 {
		update["login_at"] = table.LoginAt
	}
	// delete the templates code end

	return db.WithContext(ctx).Model(table).Updates(update).Error
}

// GetBy{{.ColumnNameCamel}} get a record by {{.ColumnNameCamelFCL}}
func (d *{{.TableNameCamelFCL}}Dao) GetBy{{.ColumnNameCamel}}(ctx context.Context, {{.ColumnNameCamelFCL}} {{.GoType}}) (*model.{{.TableNameCamel}}, error) {
	// no cache
	if d.cache == nil {
		record := &model.{{.TableNameCamel}}{}
		err := d.db.WithContext(ctx).Where("{{.ColumnName}} = ?", {{.ColumnNameCamelFCL}}).First(record).Error
		return record, err
	}

	// get from cache
	record, err := d.cache.Get(ctx, {{.ColumnNameCamelFCL}})
	if err == nil {
		return record, nil
	}

	// get from database
	if errors.Is(err, database.ErrCacheNotFound) {
		// for the same {{.ColumnNameCamelFCL}}, prevent high concurrent simultaneous access to database
		{{if .IsStringType}}val, err, _ := d.sfg.Do({{.ColumnNameCamelFCL}}, func() (interface{}, error) {
{{else}}		val, err, _ := d.sfg.Do(utils.{{.GoTypeFCU}}ToStr({{.ColumnNameCamelFCL}}), func() (interface{}, error) {
{{end}}
			table := &model.{{.TableNameCamel}}{}
			err = d.db.WithContext(ctx).Where("{{.ColumnName}} = ?", {{.ColumnNameCamelFCL}}).First(table).Error
			if err != nil {
				// set placeholder cache to prevent cache penetration, default expiration time 10 minutes
				if errors.Is(err, database.ErrRecordNotFound) {
					if err = d.cache.SetPlaceholder(ctx, {{.ColumnNameCamelFCL}}); err != nil {
						logger.Warn("cache.SetPlaceholder error", logger.Err(err), logger.Any("{{.ColumnNameCamelFCL}}", {{.ColumnNameCamelFCL}}))
					}
					return nil, database.ErrRecordNotFound
				}
				return nil, err
			}
			// set cache
			if err = d.cache.Set(ctx, {{.ColumnNameCamelFCL}}, table, cache.{{.TableNameCamel}}ExpireTime); err != nil {
				logger.Warn("cache.Set error", logger.Err(err), logger.Any("{{.ColumnNameCamelFCL}}", {{.ColumnNameCamelFCL}}))
			}
			return table, nil
		})
		if err != nil {
			return nil, err
		}
		table, ok := val.(*model.{{.TableNameCamel}})
		if !ok {
			return nil, database.ErrRecordNotFound
		}
		return table, nil
	}

	if d.cache.IsPlaceholderErr(err) {
		return nil, database.ErrRecordNotFound
	}

	return nil, err
}

// GetByColumns get paging records by column information,
// Note: query performance degrades when table rows are very large because of the use of offset.
//
// params includes paging parameters and query parameters
// paging parameters (required):
//
//	page: page number, starting from 0
//	limit: lines per page
//	sort: sort fields, default is {{.ColumnNameCamelFCL}} backwards, you can add - sign before the field to indicate reverse order, no - sign to indicate ascending order, multiple fields separated by comma
//
// query parameters (not required):
//
//	name: column name
//  exp: expressions, which default is "=",  support =, !=, >, >=, <, <=, like, in, notin, isnull, isnotnull
//	value: column value, if exp=in, multiple values are separated by commas
//	logic: logical type, defaults to and when value is null, only &(and), ||(or)
//
// example: search for a male over 20 years of age
//
//	params = &query.Params{
//	    Page: 0,
//	    Limit: 20,
//	    Columns: []query.Column{
//		{
//			Name:    "age",
//			Exp: ">",
//			Value:   20,
//		},
//		{
//			Name:  "gender",
//			Value: "male",
//		},
//	}
func (d *{{.TableNameCamelFCL}}Dao) GetByColumns(ctx context.Context, params *query.Params) ([]*model.{{.TableNameCamel}}, int64, error) {
	queryStr, args, err := params.ConvertToGormConditions()
	if err != nil {
		return nil, 0, errors.New("query params error: " + err.Error())
	}

	// 生成唯一 key
    key := "columns:" + gocrypto.Md5([]byte(fmt.Sprintf("%s_%v", queryStr, args)))

	var result struct {
		records []*model.{{.TableNameCamel}}
		total   int64
	}

	// 使用 singleflight 避免并发重复查询
	val, err, _ := d.sfg.Do(key, func() (interface{}, error) {
		var total int64
		var records []*model.{{.TableNameCamel}}

		// 统计总数（若需要）
		if params.Sort != "ignore count" {
			err := d.db.WithContext(ctx).Model(&model.{{.TableNameCamel}}{}).Where(queryStr, args...).Count(&total).Error
			if err != nil {
				return nil, err
			}
			if total == 0 {
				return struct {
					records []*model.{{.TableNameCamel}}
					total   int64
				}{records: []*model.{{.TableNameCamel}}{}, total: 0}, nil
			}
		}

		// 分页查询
		order, limit, offset := params.ConvertToPage()
		err := d.db.WithContext(ctx).Order(order).Limit(limit).Offset(offset).Where(queryStr, args...).Find(&records).Error
		if err != nil {
			return nil, err
		}

		return struct {
			records []*model.{{.TableNameCamel}}
			total   int64
		}{records: records, total: total}, nil
	})

	if err != nil {
		return nil, 0, err
	}

	result = val.(struct {
		records []*model.{{.TableNameCamel}}
		total   int64
	})

	return result.records, result.total, nil
}

func (d *{{.TableNameCamelFCL}}Dao) GetOneByColumns(ctx context.Context, params *query.Params) (*model.{{.TableNameCamel}}, error) {
	queryStr, args, err := params.ConvertToGormConditions()
	if err != nil {
		return nil, errors.New("query params error: " + err.Error())
	}
    // 生成唯一 key
    key := "one_column:" + gocrypto.Md5([]byte(fmt.Sprintf("%s_%v", queryStr, args)))

	// no cache
	if d.cache == nil {
		record := &model.{{.TableNameCamel}}{}
		err := d.db.WithContext(ctx).Where(queryStr, args...).First(record).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return record, err
	}

	// 有缓存的情况
	// 先尝试从缓存获取ID
	cachedID, err := d.cache.GetIdByKey(ctx, key)
	if err == nil && cachedID != 0 {
		// 通过ID获取完整信息
		return d.GetByID(ctx, cachedID)
	}

	// 缓存中没有找到，检查是否是占位符
	if d.cache.IsPlaceholderErr(err) {
		return nil, database.ErrRecordNotFound
	}
	record := &model.{{.TableNameCamel}}{}
	// 从数据库获取
	val, err, _ := d.sfg.Do(key, func() (interface{}, error) {
		err := d.db.WithContext(ctx).Where(queryStr, args...).First(record).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// 设置占位符缓存防止缓存穿透
				if err = d.cache.SetPlaceholderByKey(ctx, key); err != nil {
					logger.Warn("cache.SetPlaceholderByKey error", logger.Err(err), logger.Any("key", key))
				}
			}
			return nil, err
		}

		// 将查询结果的ID缓存起来
		if err = d.cache.SetIdByKey(ctx, key, record.ID, cache.{{.TableNameCamel}}ExpireTime); err != nil {
			logger.Warn("cache.SetIdByKey error", logger.Err(err), logger.Any("key", key), logger.Any("id", record.ID))
		}

		return record, nil
	})

	if err != nil {
		return nil, err
	}

	record = val.(*model.{{.TableNameCamel}})
	return record, nil
}


// DeleteBy{{.ColumnNamePluralCamel}} delete records by batch {{.ColumnNameCamelFCL}}
func (d *{{.TableNameCamelFCL}}Dao) DeleteBy{{.ColumnNamePluralCamel}}(ctx context.Context, {{.ColumnNamePluralCamelFCL}} []{{.GoType}}) error {
	defer func() {
		_ = d.deleteCache(ctx, 0)
	}()
	err := d.db.WithContext(ctx).Where("{{.ColumnName}} IN (?)", {{.ColumnNamePluralCamelFCL}}).Delete(&model.{{.TableNameCamel}}{}).Error
	if err != nil {
		return err
	}

	// delete cache
	for _, {{.ColumnNameCamelFCL}} := range {{.ColumnNamePluralCamelFCL}} {
		_ = d.deleteCache(ctx, {{.ColumnNameCamelFCL}})
	}

	return nil
}

func (d *{{.TableNameCamelFCL}}Dao) DeleteByCondition(ctx context.Context, c *query.Conditions) error {
	defer func() {
		// delete cache
		_ = d.deleteCache(ctx, 88888888)
	}()
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return err
	}
	err = d.db.WithContext(ctx).Where(queryStr, args...).Delete(&model.{{.TableNameCamel}}{}).Error
	if err != nil {
		return err
	}
	return nil
}

// GetByCondition get a record by condition
// query conditions:
//
//	name: column name
//  exp: expressions, which default is "=",  support =, !=, >, >=, <, <=, like, in, notin, isnull, isnotnull
//	value: column value, if exp=in, multiple values are separated by commas
//	logic: logical type, defaults to and when value is null, only &(and), ||(or)
//
// example: find a male aged 20
//
//	condition = &query.Conditions{
//	    Columns: []query.Column{
//		{
//			Name:    "age",
//			Value:   20,
//		},
//		{
//			Name:  "gender",
//			Value: "male",
//		},
//	}
func (d *{{.TableNameCamelFCL}}Dao) GetByCondition(ctx context.Context, c *query.Conditions) (ids []uint64, err error)  {
queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return nil, err
	}
	var tables []*model.{{.TableNameCamel}}{}
	key := "condition:" + gocrypto.Md5([]byte(fmt.Sprintf("%s_%v", queryStr, args)))
	if d.cache == nil {
		// for the same id, prevent high concurrent simultaneous access to database
		val, err, _ := d.sfg.Do(key, func() (interface{}, error) {
			err = d.db.WithContext(ctx).Where(queryStr, args...).Find(&tables).Error
			if err != nil {
				return nil, err
			}
			for _, table := range tables {
				ids = append(ids, table.ID)
			}
			return ids, nil
		})
		if err != nil {
			return nil, err
		}
		ids, ok := val.([]uint64)
		if !ok {
			return nil, database.ErrRecordNotFound
		}
		return ids, nil
	}

	// get from cache
	ids, err = d.cache.GetIdsByKey(ctx, key)
	if err == nil {
		return ids, nil
	}
	// get from database
	if errors.Is(err, database.ErrCacheNotFound) {
		// for the same id, prevent high concurrent simultaneous access to database
		val, err, _ := d.sfg.Do(key, func() (interface{}, error) {
			err = d.db.WithContext(ctx).Where(queryStr, args...).Find(&tables).Error
			if err != nil {
				// set placeholder cache to prevent cache penetration, default expiration time 10 minutes
				if errors.Is(err, database.ErrRecordNotFound) {
					if err = d.cache.SetPlaceholderByKey(ctx, key); err != nil {
						logger.Warn("cache.SetPlaceholderByKey error", logger.Err(err), logger.Any("key", key))
					}
					return nil, database.ErrRecordNotFound
				}
				return nil, err
			}
			for _, table := range tables {
				ids = append(ids, table.ID)
			}
			// set cache
			if err = d.cache.SetIdsByKey(ctx, key, ids, cache.{{.TableNameCamel}}ExpireTime); err != nil {
				logger.Warn("cache.Set error", logger.Err(err), logger.Any("ids", ids))
			}
			return ids, nil
		})
		if err != nil {
			return nil, err
		}
		ids, ok := val.([]uint64)
		if !ok {
			return nil, database.ErrRecordNotFound
		}
		return ids, nil
	}
	if d.cache.IsPlaceholderErr(err) {
		return nil, database.ErrRecordNotFound
	}
	return nil, err
}

// GetBy{{.ColumnNamePluralCamel}} get records by batch {{.ColumnNameCamelFCL}}
func (d *{{.TableNameCamelFCL}}Dao) GetBy{{.ColumnNamePluralCamel}}(ctx context.Context, {{.ColumnNamePluralCamelFCL}} []{{.GoType}}) (map[{{.GoType}}]*model.{{.TableNameCamel}}, error) {
	// no cache
	if d.cache == nil {
		var records []*model.{{.TableNameCamel}}
		err := d.db.WithContext(ctx).Where("{{.ColumnName}} IN (?)", {{.ColumnNamePluralCamelFCL}}).Find(&records).Error
		if err != nil {
			return nil, err
		}
		itemMap := make(map[{{.GoType}}]*model.{{.TableNameCamel}})
		for _, record := range records {
			itemMap[record.{{.ColumnNameCamel}}] = record
		}
		return itemMap, nil
	}

	// get form cache
	itemMap, err := d.cache.MultiGet(ctx, {{.ColumnNamePluralCamelFCL}})
	if err != nil {
		return nil, err
	}

	var missed{{.ColumnNamePluralCamel}} []{{.GoType}}
	for _, {{.ColumnNameCamelFCL}} := range {{.ColumnNamePluralCamelFCL}} {
		if _, ok := itemMap[{{.ColumnNameCamelFCL}}]; !ok {
			missed{{.ColumnNamePluralCamel}} = append(missed{{.ColumnNamePluralCamel}}, {{.ColumnNameCamelFCL}})
		}
	}

	// get missed data
	if len(missed{{.ColumnNamePluralCamel}}) > 0 {
		// find the {{.ColumnNameCamelFCL}} of an active placeholder, i.e. an {{.ColumnNameCamelFCL}} that does not exist in database
		var realMissed{{.ColumnNamePluralCamel}} []{{.GoType}}
		for _, {{.ColumnNameCamelFCL}} := range missed{{.ColumnNamePluralCamel}} {
			_, err = d.cache.Get(ctx, {{.ColumnNameCamelFCL}})
			if d.cache.IsPlaceholderErr(err) {
				continue
			}
			realMissed{{.ColumnNamePluralCamel}} = append(realMissed{{.ColumnNamePluralCamel}}, {{.ColumnNameCamelFCL}})
		}

		if len(realMissed{{.ColumnNamePluralCamel}}) > 0 {
			var records []*model.{{.TableNameCamel}}
			var record{{.ColumnNameCamel}}Map = make(map[{{.GoType}}]struct{})
			err = d.db.WithContext(ctx).Where("{{.ColumnName}} IN (?)", realMissed{{.ColumnNamePluralCamel}}).Find(&records).Error
			if err != nil {
				return nil, err
			}

			if len(records) > 0 {
				for _, record := range records {
					itemMap[record.{{.ColumnNameCamel}}] = record
					record{{.ColumnNameCamel}}Map[record.{{.ColumnNameCamel}}] = struct{}{}
				}
				err = d.cache.MultiSet(ctx, records, cache.{{.TableNameCamel}}ExpireTime)
				if err != nil {
					logger.Warn("cache.MultiSet error", logger.Err(err), logger.Any("{{.ColumnNamePluralCamelFCL}}", records))
				}
				if len(records) == len(realMissed{{.ColumnNamePluralCamel}}) {
					return itemMap, nil
				}
			}
			for _, {{.ColumnNameCamelFCL}} := range realMissed{{.ColumnNamePluralCamel}} {
				if _, ok := record{{.ColumnNameCamel}}Map[{{.ColumnNameCamelFCL}}]; !ok {
					if err = d.cache.SetPlaceholder(ctx, {{.ColumnNameCamelFCL}}); err != nil {
						logger.Warn("cache.SetPlaceholder error", logger.Err(err), logger.Any("{{.ColumnNameCamelFCL}}", {{.ColumnNameCamelFCL}}))
					}
				}
			}
		}
	}

	return itemMap, nil
}

// GetByLast{{.ColumnNameCamel}} get paging records by last {{.ColumnNameCamelFCL}} and limit
func (d *{{.TableNameCamelFCL}}Dao) GetByLast{{.ColumnNameCamel}}(ctx context.Context, last{{.ColumnNameCamel}} {{.GoType}}, limit int, sort string) ([]*model.{{.TableNameCamel}}, error) {
	if sort == "" {
		sort = "-{{.ColumnName}}"
	}
	page := query.NewPage(0, limit, sort)

	records := []*model.{{.TableNameCamel}}{}
	err := d.db.WithContext(ctx).Order(page.Sort()).Limit(page.Limit()).Where("{{.ColumnName}} < ?", last{{.ColumnNameCamel}}).Find(&records).Error
	if err != nil {
		return nil, err
	}
	return records, nil
}

// CreateByTx create a record in the database using the provided transaction
func (d *{{.TableNameCamelFCL}}Dao) CreateByTx(ctx context.Context, tx *gorm.DB, table *model.{{.TableNameCamel}}) ({{.GoType}}, error) {
	defer func() {
		_ = d.deleteCache(ctx, 0)
	}()
	err := tx.WithContext(ctx).Create(table).Error
	return table.{{.ColumnNameCamel}}, err
}

func (d *{{.TableNameCamelFCL}}Dao) CreateByTxInBatches(ctx context.Context, tx *gorm.DB, tables []*model.{{.TableNameCamel}}, batchSize int) error {
	defer func() {
		_ = d.deleteCache(ctx, 0)
	}()
    return tx.WithContext(ctx).CreateInBatches(tables, batchSize).Error
}

// DeleteByTx delete a record by {{.ColumnNameCamelFCL}} in the database using the provided transaction
func (d *{{.TableNameCamelFCL}}Dao) DeleteByTx(ctx context.Context, tx *gorm.DB, {{.ColumnNameCamelFCL}} {{.GoType}}) error {
	defer func() {
		_ = d.deleteCache(ctx, 0)
	}()
	update := map[string]interface{}{
		"deleted_at": time.Now(),
	}
	err := tx.WithContext(ctx).Model(&model.{{.TableNameCamel}}{}).Where("{{.ColumnName}} = ?", {{.ColumnNameCamelFCL}}).Updates(update).Error
	if err != nil {
		return err
	}

	// delete cache
	_ = d.deleteCache(ctx, {{.ColumnNameCamelFCL}})

	return nil
}

func (d *{{.TableNameCamelFCL}}Dao) DeleteByTxCondition(ctx context.Context, tx *gorm.DB, c *query.Conditions) error {
	defer func() {
		// delete cache
		_ = d.deleteCache(ctx, 88888888)
	}()
	queryStr, args, err := c.ConvertToGorm()
	if err != nil {
		return err
	}
	err = tx.WithContext(ctx).Where(queryStr, args...).Delete(&model.{{.TableNameCamel}}{}).Error
	if err != nil {
		return err
	}
	return nil
}

// UpdateByTx update a record by {{.ColumnNameCamelFCL}} in the database using the provided transaction
	defer func() {
		_ = d.deleteCache(ctx, 0)
	}()
	err := d.updateDataBy{{.ColumnNameCamel}}(ctx, tx, table)

	// delete cache
	_ = d.deleteCache(ctx, table.{{.ColumnNameCamel}})

	return err
}

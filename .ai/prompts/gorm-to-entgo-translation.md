# GORM to entgo Translation Guide

This document provides patterns and examples for translating upstream GORM code to local entgo code.

## Common Translation Patterns

### Basic Operations

| GORM | entgo |
|------|-------|
| `db.Create(&model)` | `client.Entity.Create().SetX().Save()` |
| `db.First(&model, id)` | `client.Entity.Get(ctx, id)` |
| `db.Where("email = ?", e).First(&m)` | `client.Entity.Query().Where(entity.Email(e)).First(ctx)` |
| `db.Model(&m).Updates(map)` | `client.Entity.UpdateOneID(id).SetX().Save(ctx)` |
| `db.Delete(&m, id)` | `client.Entity.DeleteOneID(id).Exec(ctx)` |
| `db.Where(...).Find(&list)` | `client.Entity.Query().Where(...).All(ctx)` |
| `db.Where(...).Count()` | `client.Entity.Query().Where(...).Count(ctx)` |
| `db.Preload("Relation")` | `client.Entity.Query().WithRelation().All(ctx)` |

### Schema Definition

| GORM | entgo |
|------|-------|
| `gorm:"primaryKey"` | `field.Int("id").SchemaType(...)` + `entgo.io/ent` auto |
| `gorm:"index"` | `index.Fields("field_name")` in schema |
| `gorm:"uniqueIndex"` | `index.Fields("field_name").Unique()` |
| `gorm:"type:varchar(255)"` | `field.String("name").MaxLen(255)` |
| `gorm:"not null"` | `field.String("name").NotEmpty()` |
| `gorm:"default:0"` | `field.Int("count").Default(0)` |

### Handling NotFound

**GORM pattern (upstream)**:
```go
if err := db.First(&m).Error; errors.Is(err, gorm.ErrRecordNotFound) {
    // not found
}
```

**entgo pattern (local)**:
```go
m, err := client.Entity.Query().Where(...).First(ctx)
if err != nil && !ent.IsNotFound(err) {
    return err // real error
}
if m == nil {
    // not found
}
```

**Critical**: Always check `m != nil` before accessing fields when `IsNotFound` is allowed.

## Translation Examples

### Example 1: Simple Query

**Upstream (GORM)**:
```go
var user User
if err := db.Where("email = ?", email).First(&user).Error; err != nil {
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return nil, xerr.NewErrCode(xerr.UserNotExist)
    }
    return nil, err
}
return &user, nil
```

**Local (entgo)**:
```go
user, err := client.User.Query().
    Where(user.Email(email)).
    First(ctx)
if err != nil && !ent.IsNotFound(err) {
    return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query user failed")
}
if user == nil {
    return nil, xerr.NewErrCode(xerr.UserNotExist)
}
return user, nil
```

### Example 2: Create Record

**Upstream (GORM)**:
```go
user := User{
    Email: email,
    Password: hashedPassword,
    CreatedAt: time.Now(),
}
if err := db.Create(&user).Error; err != nil {
    return nil, err
}
return &user, nil
```

**Local (entgo)**:
```go
user, err := client.User.Create().
    SetEmail(email).
    SetPassword(hashedPassword).
    Save(ctx)
if err != nil {
    return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseInsertError), "create user failed")
}
return user, nil
```

### Example 3: Update Record

**Upstream (GORM)**:
```go
if err := db.Model(&user).Updates(map[string]interface{}{
    "password": newPassword,
    "updated_at": time.Now(),
}).Error; err != nil {
    return err
}
```

**Local (entgo)**:
```go
_, err := client.User.UpdateOneID(user.ID).
    SetPassword(newPassword).
    Save(ctx)
if err != nil {
    return errors.Wrapf(xerr.NewErrCode(xerr.DatabaseUpdateError), "update user failed")
}
```

### Example 4: Query with Relations

**Upstream (GORM)**:
```go
var orders []Order
db.Preload("User").Preload("Payment").Where("status = ?", status).Find(&orders)
```

**Local (entgo)**:
```go
orders, err := client.Order.Query().
    WithUser().
    WithPayment().
    Where(order.Status(status)).
    All(ctx)
if err != nil {
    return nil, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "query orders failed")
}
```

### Example 5: Count and Aggregate

**Upstream (GORM)**:
```go
var count int64
db.Model(&Order{}).Where("user_id = ?", userID).Count(&count)

var totalAmount float64
db.Model(&Order{}).Where("user_id = ?", userID).Select("SUM(amount)").Scan(&totalAmount)
```

**Local (entgo)**:
```go
count, err := client.Order.Query().
    Where(order.UserID(userID)).
    Count(ctx)
if err != nil {
    return 0, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "count orders failed")
}

// For aggregation, use raw SQL or entgo's Aggregate
totalAmount, err := client.Order.Query().
    Where(order.UserID(userID)).
    Aggregate(ent.Sum(order.FieldAmount)).
    Int(ctx)
if err != nil {
    return 0, errors.Wrapf(xerr.NewErrCode(xerr.DatabaseQueryError), "sum orders failed")
}
```

## Schema Translation

### Example: Adding a New Field

**Upstream migration (SQL)**:
```sql
ALTER TABLE `user` ADD COLUMN `avatar_url` VARCHAR(500) DEFAULT '' COMMENT 'User avatar URL';
```

**Local entgo schema**:
```go
// ent/schema/user.go
func (User) Fields() []ent.Field {
    return []ent.Field{
        // ... existing fields ...
        field.String("avatar_url").
            Default("").
            MaxLen(500).
            Comment("User avatar URL"),
    }
}
```

Then run:
```bash
go generate ./ent
```

### Example: Adding an Index

**Upstream migration (SQL)**:
```sql
CREATE INDEX idx_user_email ON `user` (`email`);
```

**Local entgo schema**:
```go
// ent/schema/user.go
func (User) Indexes() []ent.Index {
    return []ent.Index{
        // ... existing indexes ...
        index.Fields("email"),
    }
}
```

Then run:
```bash
go generate ./ent
```

## Tips

1. **Always check for nil**: entgo returns `(nil, NotFound)` not `(zero_value, nil)`
2. **Use entgo predicates**: Instead of raw SQL strings, use generated predicates like `user.Email(email)`
3. **Leverage code generation**: Run `go generate ./ent` after schema changes
4. **Test thoroughly**: entgo's type safety catches errors at compile time, but test runtime behavior
5. **Check table names**: Upstream and local may use different table names

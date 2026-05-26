# krand

Go 语言的密码学安全随机数据生成库。

## ✨ 特性

- 🔒 **密码学安全**：使用 `crypto/rand` 而非 `math/rand`
- 🧵 **线程安全**：支持 goroutine 并发使用
- 🎯 **类型安全**：强类型设计，清晰的 API 契约
- 📝 **文档完善**：全面的示例和文档
- ✅ **测试完备**：高测试覆盖率，包含并发测试

<br>

## 📦 安装

```bash
go get github.com/18721889353/sunshine/pkg/krand
```

<br>

## 🚀 快速开始

### 1️⃣ 生成随机字符串

#### 基础用法

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    // 字符类型常量
    // RNum   - 数字字符 (0-9)
    // RUpper - 大写字母 (A-Z)
    // RLower - 小写字母 (a-z)
    // RAll   - 所有字符类型（数字 + 大写 + 小写）

    // 默认长度为 6
    fmt.Println(krand.String(krand.RAll))           // 例如: "aB3kL9"
    fmt.Println(krand.String(krand.RNum, 10))       // 例如: "1234567890"
    fmt.Println(krand.String(krand.RUpper, 8))      // 例如: "ABCDEFGH"
    fmt.Println(krand.String(krand.RLower, 12))     // 例如: "abcdefghijkl"
}
```

#### 组合字符类型

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    // 使用位运算 OR 组合不同类型的字符
    
    // 数字 + 小写字母（适合生成验证码）
    code := krand.String(krand.RNum|krand.RLower, 6)
    fmt.Println("验证码:", code)  // 例如: "abc123"
    
    // 数字 + 大写字母（适合生成订单号）
    orderNo := krand.String(krand.RNum|krand.RUpper, 12)
    fmt.Println("订单号:", orderNo)  // 例如: "AB12CD34EF56"
    
    // 大写字母 + 小写字母（适合生成用户名）
    username := krand.String(krand.RUpper|krand.RLower, 8)
    fmt.Println("用户名:", username)  // 例如: "AbCdEfGh"
    
    // 所有字符类型（适合生成强密码）
    password := krand.String(krand.RAll, 16)
    fmt.Println("密码:", password)  // 例如: "aB3kL9mN2pQ5rT8w"
}
```

#### 获取原始字节

```go
package main

import (
    "encoding/base64"
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    // 获取随机字节切片
    bytes := krand.Bytes(krand.RAll, 32)
    fmt.Printf("随机字节: %v\n", bytes)
    
    // 转换为 Base64 编码（适合生成 Token）
    token := base64.URLEncoding.EncodeToString(bytes)
    fmt.Println("Token:", token)
}
```

#### 实际应用场景

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    // 场景1: 生成短信验证码（6位数字）
    smsCode := krand.String(krand.RNum, 6)
    fmt.Println("短信验证码:", smsCode)  // 例如: "123456"
    
    // 场景2: 生成邀请码（8位大写字母+数字）
    inviteCode := krand.String(krand.RNum|krand.RUpper, 8)
    fmt.Println("邀请码:", inviteCode)  // 例如: "A1B2C3D4"
    
    // 场景3: 生成临时密码（12位混合字符）
    tempPassword := krand.String(krand.RAll, 12)
    fmt.Println("临时密码:", tempPassword)  // 例如: "aB3kL9mN2pQ5"
    
    // 场景4: 生成文件后缀（4位小写字母）
    fileSuffix := krand.String(krand.RLower, 4)
    fmt.Println("文件后缀:", fileSuffix)  // 例如: "abcd"
}
```

<br>

### 2️⃣ 生成随机整数

#### 基础用法

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    // 默认范围 [0, 100]
    num1 := krand.Int()
    fmt.Println("随机数 [0-100]:", num1)  // 例如: 42
    
    // 指定最大值 [0, max]
    num2 := krand.Int(200)
    fmt.Println("随机数 [0-200]:", num2)  // 例如: 156
    
    // 指定范围 [min, max]
    num3 := krand.Int(1000, 2000)
    fmt.Println("随机数 [1000-2000]:", num3)  // 例如: 1523
}
```

#### 自动交换参数

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    // 即使 min > max，也会自动交换
    num := krand.Int(2000, 1000)
    fmt.Println("随机数 [1000-2000]:", num)  // 例如: 1523
    
    // 无需担心参数顺序
    nums := []int{
        krand.Int(10, 20),
        krand.Int(20, 10),  // 结果相同
    }
    fmt.Println("两个随机数:", nums)
}
```

#### 实际应用场景

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    // 场景1: 模拟掷骰子（1-6）
    dice := krand.Int(1, 6)
    fmt.Println("骰子点数:", dice)
    
    // 场景2: 抽奖概率（0-100，小于10表示中奖）
    lottery := krand.Int(0, 100)
    if lottery < 10 {
        fmt.Println("恭喜中奖！")
    } else {
        fmt.Println("未中奖")
    }
    
    // 场景3: 随机延迟（100-500毫秒）
    delayMs := krand.Int(100, 500)
    fmt.Printf("随机延迟: %d 毫秒\n", delayMs)
    
    // 场景4: 随机分页大小（10-50条）
    pageSize := krand.Int(10, 50)
    fmt.Printf("分页大小: %d 条\n", pageSize)
    
    // 场景5: 随机端口号（8000-9000）
    port := krand.Int(8000, 9000)
    fmt.Printf("随机端口: %d\n", port)
}
```

#### 边界情况处理

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    // 负数参数会返回 0
    num := krand.Int(-5)
    fmt.Println("负数参数:", num)  // 输出: 0
    
    // 相同的最小值和最大值
    num2 := krand.Int(100, 100)
    fmt.Println("相同范围:", num2)  // 输出: 100
}
```

<br>

### 3️⃣ 生成随机浮点数

#### 基础用法

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    // 格式: Float64(小数位数, [范围参数...])
    
    // 默认范围 [0.0, 100.0]，0位小数
    num1 := krand.Float64(0)
    fmt.Printf("随机数: %.1f\n", num1)  // 例如: 42.0
    
    // 范围 [0.0, 200.0]，1位小数
    num2 := krand.Float64(1, 200)
    fmt.Printf("随机数: %.1f\n", num2)  // 例如: 123.4
    
    // 范围 [100.0, 1000.0]，2位小数
    num3 := krand.Float64(2, 100, 1000)
    fmt.Printf("随机数: %.2f\n", num3)  // 例如: 456.78
}
```

#### 自动交换参数

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    // min > max 时自动交换
    num := krand.Float64(2, 1000, 100)
    fmt.Printf("随机数 [100-1000]: %.2f\n", num)  // 例如: 456.78
}
```

#### 实际应用场景

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    // 场景1: 模拟商品价格（10.00 - 999.99）
    price := krand.Float64(2, 10, 999)
    fmt.Printf("商品价格: ¥%.2f\n", price)
    
    // 场景2: 模拟温度（-10.0 - 40.0）
    temperature := krand.Float64(1, -10, 40)
    fmt.Printf("当前温度: %.1f°C\n", temperature)
    
    // 场景3: 模拟折扣率（0.1 - 0.9，即1折到9折）
    discount := krand.Float64(2, 1, 9)
    fmt.Printf("折扣率: %.2f 折\n", discount/10)
    
    // 场景4: 模拟体重（50.0 - 100.0 kg）
    weight := krand.Float64(1, 50, 100)
    fmt.Printf("体重: %.1f kg\n", weight)
    
    // 场景5: 模拟成绩（0.0 - 100.0）
    score := krand.Float64(1, 0, 100)
    fmt.Printf("成绩: %.1f 分\n", score)
    
    // 场景6: 模拟汇率（6.0000 - 7.5000）
    exchangeRate := krand.Float64(4, 6, 7)
    fmt.Printf("汇率: %.4f\n", exchangeRate)
}
```

#### 精度控制

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    // 不同小数位数的对比
    fmt.Println("0位小数:", krand.Float64(0, 0, 100))   // 例如: 42
    fmt.Println("1位小数:", krand.Float64(1, 0, 100))   // 例如: 42.3
    fmt.Println("2位小数:", krand.Float64(2, 0, 100))   // 例如: 42.35
    fmt.Println("3位小数:", krand.Float64(3, 0, 100))   // 例如: 42.356
    fmt.Println("4位小数:", krand.Float64(4, 0, 100))   // 例如: 42.3567
    
    // 超过10位小数会自动限制为10位
    fmt.Println("10位小数:", krand.Float64(15, 0, 100)) // 例如: 42.3567891234
}
```
<br>

### 4️⃣ 生成唯一 ID

#### NewID - 数字 ID

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    // 基于时间戳（毫秒）+ 随机数生成
    // 格式: timestamp_ms * 1000000 + random(0-999999)
    // 长度: 19位数字
    
    id := krand.NewID()
    fmt.Println("数字 ID:", id)  // 例如: 1701234567890397409
    
    // 适合用作数据库主键
    fmt.Printf("ID 类型: %T\n", id)  // int64
}
```

#### NewStringID - 十六进制字符串 ID

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    // NewID() 的十六进制表示形式
    // 长度: 固定 16 个字符
    
    stringID := krand.NewStringID()
    fmt.Println("字符串 ID:", stringID)  // 例如: "179bffd372b8e8e1"
    
    // 适合用作 URL 参数、文件名等
    filename := fmt.Sprintf("upload_%s.jpg", stringID)
    fmt.Println("文件名:", filename)  // 例如: "upload_179bffd372b8e8e1.jpg"
}
```

#### NewSeriesID - 可读系列 ID

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    // 人类可读的系列 ID，包含微秒级精度时间戳
    // 格式: YYYYMMDDHHmmssffffffRRRRRR（共 26 个字符）
    //   - 前 20 个字符: 微秒级时间戳（YYYYMMDDHHmmssffffff）
    //   - 后 6 个字符: 随机数字字符串
    
    seriesID := krand.NewSeriesID()
    fmt.Println("系列 ID:", seriesID)  // 例如: "20240102150405123456789012"
    
    // 解析时间戳部分
    timestampPart := seriesID[:20]
    randomPart := seriesID[20:]
    fmt.Println("时间戳部分:", timestampPart)  // 例如: "20240102150405123456"
    fmt.Println("随机部分:", randomPart)       // 例如: "789012"
}
```

#### 实际应用场景

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    // 场景1: 数据库主键（使用 NewID）
    userID := krand.NewID()
    fmt.Printf("用户 ID: %d\n", userID)
    // SQL: INSERT INTO users (id, name) VALUES (?, ?)
    
    // 场景2: 订单号（使用 NewSeriesID，可读性好）
    orderNo := krand.NewSeriesID()
    fmt.Printf("订单号: %s\n", orderNo)
    // 例如: "20240102150405123456789012"
    // 可以直观看出创建时间：2024年01月02日 15:04:05.123456
    
    // 场景3: 文件上传唯一文件名（使用 NewStringID）
    fileID := krand.NewStringID()
    originalFilename := "photo.jpg"
    uniqueFilename := fmt.Sprintf("%s_%s", fileID, originalFilename)
    fmt.Printf("唯一文件名: %s\n", uniqueFilename)
    // 例如: "179bffd372b8e8e1_photo.jpg"
    
    // 场景4: 会话 ID（使用 NewStringID）
    sessionID := krand.NewStringID()
    fmt.Printf("会话 ID: %s\n", sessionID)
    // 存储在 Redis: SET session:{sessionID} user_data
    
    // 场景5: 日志追踪 ID（使用 NewSeriesID）
    traceID := krand.NewSeriesID()
    fmt.Printf("追踪 ID: %s\n", traceID)
    // 可以在日志中关联同一请求的所有日志
}
```

#### ID 选择指南

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    fmt.Println("=== ID 类型对比 ===\n")
    
    // NewID: 最短，适合数据库存储
    fmt.Println("1. NewID (数字):")
    fmt.Printf("   长度: 19 位数字\n")
    fmt.Printf("   示例: %d\n", krand.NewID())
    fmt.Printf("   适用: 数据库主键、需要数值比较的场景\n\n")
    
    // NewStringID: 紧凑，适合 URL 和文件名
    fmt.Println("2. NewStringID (十六进制):")
    fmt.Printf("   长度: 16 个字符\n")
    fmt.Printf("   示例: %s\n", krand.NewStringID())
    fmt.Printf("   适用: URL 参数、文件名、Token\n\n")
    
    // NewSeriesID: 最长，但可读性最好
    fmt.Println("3. NewSeriesID (时间戳+随机):")
    fmt.Printf("   长度: 26 个字符\n")
    fmt.Printf("   示例: %s\n", krand.NewSeriesID())
    fmt.Printf("   适用: 订单号、流水号、需要人工查看的场景\n")
}
```

#### 并发安全性测试

```go
package main

import (
    "fmt"
    "sync"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    // 测试并发生成 ID 的唯一性
    var wg sync.WaitGroup
    ids := make(map[int64]bool)
    mu := sync.Mutex{}
    
    // 启动 100 个 goroutine，每个生成 10 个 ID
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 10; j++ {
                id := krand.NewID()
                mu.Lock()
                ids[id] = true
                mu.Unlock()
            }
        }()
    }
    
    wg.Wait()
    fmt.Printf("生成了 %d 个唯一 ID\n", len(ids))  // 应该是 1000
}
```

<br>

## 🔧 高级用法

### 线程安全

所有函数都是线程安全的，可以并发使用：

```go
package main

import (
    "fmt"
    "sync"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    var wg sync.WaitGroup
    
    // 启动 100 个 goroutine 并发调用
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            
            // 所有函数都可以安全地并发调用
            str := krand.String(krand.RAll, 16)
            num := krand.Int(1000)
            f := krand.Float64(2, 100)
            id1 := krand.NewID()
            id2 := krand.NewStringID()
            id3 := krand.NewSeriesID()
            
            fmt.Printf("Goroutine %d: str=%s, num=%d\n", id, str, num)
        }(i)
    }
    
    wg.Wait()
    fmt.Println("所有 goroutine 完成")
}
```

### 错误处理

库使用 `crypto/rand`，极少数情况下可能失败。在这种情况下：
- String/Bytes 函数回退到安全的默认值
- Int/Float64 函数返回范围最小值
- ID 生成函数优雅地处理错误

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

func main() {
    // crypto/rand 失败的概率极低（几乎不可能）
    // 但库已经做了容错处理，无需额外错误检查
    
    // 即使系统熵池不足，也会返回安全值
    str := krand.String(krand.RAll, 10)
    fmt.Println("随机字符串:", str)
    
    // 不会 panic，不会返回错误
    num := krand.Int(100)
    fmt.Println("随机数:", num)
}
```

<br>

## ⚡ 性能说明

虽然 `crypto/rand` 比 `math/rand` 稍慢，但它提供：
- ✅ 密码学安全性（适合令牌、密码等敏感数据）
- ✅ 无需管理种子
- ✅ 更好的随机性质量

### 基准测试结果

```
BenchmarkInt-8                   4,948,249    256.6 ns/op    48 B/op    3 allocs/op
BenchmarkFloat64-8               2,053,057    574.2 ns/op    96 B/op    6 allocs/op
BenchmarkString_ALL_6-8            906,398   1339 ns/op     288 B/op   19 allocs/op
BenchmarkNewID-8                 4,695,532    252.7 ns/op    48 B/op    3 allocs/op
BenchmarkNewSeriesID-8             469,269   2512 ns/op     352 B/op   22 allocs/op
```

### 性能优化建议

对于非安全关键的高性能场景，可以考虑缓存结果：

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

// 预生成一批 ID，减少频繁调用
var idPool = make([]string, 1000)

func init() {
    for i := range idPool {
        idPool[i] = krand.NewStringID()
    }
}

func getNextID() string {
    // 从池中获取，速度更快
    return idPool[krand.Int(0, 999)]
}

func main() {
    id := getNextID()
    fmt.Println("ID:", id)
}
```

<br>

## 💡 最佳实践

### 1. 选择合适的字符集

```go
// ✅ 推荐：选择最窄的字符集
krand.String(krand.RNum, 6)           // 验证码：只需数字
krand.String(krand.RNum|krand.RUpper, 8)  // 邀请码：数字+大写

// ❌ 不推荐：过度使用 RAll
krand.String(krand.RAll, 6)  // 验证码不需要小写字母
```

### 2. 验证输入长度

```go
// ✅ 推荐：在应用层验证
length := getUserInput()  // 从用户输入获取
if length < 6 || length > 32 {
    length = 16  // 使用默认值
}
code := krand.String(krand.RAll, length)

// 库本身会处理边界情况，但最好在业务逻辑中验证
```

### 3. 不要重用 ID

```go
// ✅ 推荐：每次调用生成新值
orderID1 := krand.NewSeriesID()
orderID2 := krand.NewSeriesID()  // 不同的 ID

// ❌ 不推荐：缓存并重复使用
cachedID := krand.NewSeriesID()
// ... 多次使用 cachedID
```

### 4. 高频生成考虑批量化

```go
// ✅ 推荐：批量生成
func generateBatchIDs(count int) []string {
    ids := make([]string, count)
    for i := 0; i < count; i++ {
        ids[i] = krand.NewStringID()
    }
    return ids
}

// ❌ 不推荐：循环中逐个生成（频繁系统调用）
for i := 0; i < 1000; i++ {
    id := krand.NewStringID()  // 每次都调用 crypto/rand
    process(id)
}
```

### 5. 根据场景选择 ID 类型

```go
// 数据库主键 -> NewID (int64, 存储效率高)
userID := krand.NewID()

// URL 参数 -> NewStringID (16字符，紧凑)
shortURL := fmt.Sprintf("/s/%s", krand.NewStringID())

// 订单号 -> NewSeriesID (可读性好，含时间信息)
orderNo := krand.NewSeriesID()

// 会话 Token -> NewStringID (足够唯一，长度适中)
sessionToken := krand.NewStringID()
```

<br>

## 🔄 迁移指南

如果从旧 API 升级：

```go
// ❌ 旧 API（已废弃）
krand.String(krand.R_NUM, 10)     // 下划线命名
krand.String(krand.R_UPPER, 10)
krand.String(krand.R_LOWER, 10)
krand.String(krand.R_All, 10)     // 大小写混乱

// ✅ 新 API（推荐）
krand.String(krand.RNum, 10)      // 驼峰命名，符合 Go 规范
krand.String(krand.RUpper, 10)
krand.String(krand.RLower, 10)
krand.String(krand.RAll, 10)
```

**迁移步骤：**
1. 全局查找替换常量名：`R_NUM` → `RNum`，`R_UPPER` → `RUpper`，`R_LOWER` → `RLower`，`R_All` → `RAll`
2. 函数签名完全兼容，无需修改调用代码
3. 运行测试确保功能正常

<br>

## 📊 完整示例项目

### 示例：用户注册系统

```go
package main

import (
    "fmt"
    "time"
    "github.com/18721889353/sunshine/pkg/krand"
)

type User struct {
    ID           int64
    Username     string
    Password     string
    VerifyCode   string
    InviteCode   string
    CreatedAt    time.Time
}

func RegisterUser(username string) *User {
    user := &User{
        // 使用 NewID 作为数据库主键
        ID: krand.NewID(),
        
        Username: username,
        
        // 生成 12 位强密码
        Password: krand.String(krand.RAll, 12),
        
        // 生成 6 位数字验证码
        VerifyCode: krand.String(krand.RNum, 6),
        
        // 生成 8 位邀请码（数字+大写）
        InviteCode: krand.String(krand.RNum|krand.RUpper, 8),
        
        CreatedAt: time.Now(),
    }
    
    return user
}

func main() {
    user := RegisterUser("john_doe")
    
    fmt.Println("=== 用户注册成功 ===")
    fmt.Printf("用户 ID: %d\n", user.ID)
    fmt.Printf("用户名: %s\n", user.Username)
    fmt.Printf("临时密码: %s\n", user.Password)
    fmt.Printf("验证码: %s\n", user.VerifyCode)
    fmt.Printf("邀请码: %s\n", user.InviteCode)
    fmt.Printf("注册时间: %s\n", user.CreatedAt.Format("2006-01-02 15:04:05"))
}
```

### 示例：订单系统

```go
package main

import (
    "fmt"
    "github.com/18721889353/sunshine/pkg/krand"
)

type Order struct {
    OrderNo     string
    UserID      int64
    Amount      float64
    Status      string
}

func CreateOrder(userID int64, amount float64) *Order {
    order := &Order{
        // 使用 NewSeriesID 作为订单号（可读性好）
        OrderNo: krand.NewSeriesID(),
        UserID:  userID,
        Amount:  amount,
        Status:  "PENDING",
    }
    
    return order
}

func main() {
    order := CreateOrder(123456789, 299.99)
    
    fmt.Println("=== 订单创建成功 ===")
    fmt.Printf("订单号: %s\n", order.OrderNo)
    fmt.Printf("用户 ID: %d\n", order.UserID)
    fmt.Printf("金额: ¥%.2f\n", order.Amount)
    fmt.Printf("状态: %s\n", order.Status)
}
```

<br>

## 📄 许可证

MIT License
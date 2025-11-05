# Black Arm 机械臂和手部控制系统

## 🎯 功能特性

### 机械臂控制
- ✅ 7个关节的实时滑动条控制
- ✅ 使能/失能、设置零点、回零功能
- ✅ PID参数调节 (位置Kp、速度Kp、速度Ki、滤波增益)
- ✅ 自动扫描电机ID

### 手部控制
- ✅ 6个手指的滑动条控制 (拇指、拇指旋转、食指、中指、无名指、小指)
- ✅ 乐器类型选择 (萨克斯SKS/唢呐SN)
- ✅ 预设位置按钮 (按压/松开/高音拇指/倍高音拇指)
- ✅ **实时保存到外部配置文件**

## 🆕 新增功能：外部配置文件更新

现在您可以通过Web界面直接修改外部YAML配置文件！

### 支持的配置项

#### 萨克斯 (SKS)
- `sks_left_press_profile` - 左手按压位置
- `sks_left_release_profile` - 左手松开位置  
- `sks_right_press_profile` - 右手按压位置
- `sks_right_release_profile` - 右手松开位置

#### 唢呐 (SN)
- `sn_left_press_profile` - 左手按压位置
- `sn_left_release_profile` - 左手松开位置
- `sn_left_high_Thumb` - 左手高音拇指
- `sn_left_high_pro_Thumb` - 左手倍高音拇指
- `sn_right_press_profile` - 右手按压位置
- `sn_right_release_profile` - 右手松开位置

## 🚀 使用方法

### 1. 启动服务器
```bash
cd blackarm_go_example
./blackarm_server
```

### 2. 访问控制界面
打开浏览器访问: `http://localhost:8080`

### 3. 手部控制操作

#### 实时控制
1. 选择手部设备 (can0右手/can1左手)
2. 选择乐器类型 (萨克斯/唢呐)
3. 拖动手指滑动条实时控制

#### 预设位置
1. 点击预设按钮快速设置位置
2. 系统会发送CAN消息控制手部

#### 保存到外部配置
1. 调整手指到理想位置
2. 点击"保存当前为XXX"按钮
3. 系统会更新外部YAML配置文件

### 4. 配置文件格式

配置文件路径: `/home/linkerhand/linkerhand/piano-dashboard/blackarm_go_example/test_config.yaml`

```yaml
# 萨克斯配置
sks_left_press_profile:   [130, 82, 211, 201, 210, 213]   
sks_left_release_profile: [170, 83, 255, 240, 255, 255]   

# 唢呐配置
sn_left_press_profile: [150, 22, 238, 229, 235, 255]
sn_left_high_Thumb: [113,3]
sn_left_high_pro_Thumb: [113,24]
```

## 🔧 CAN消息格式

### 手部控制消息
```json
{
  "interface": "can0",  // CAN接口
  "id": 39,             // 右手=39, 左手=40
  "data": [0x01, thumb, thumb_rotate, index, middle, ring, pinky]
}
```

### 机械臂控制消息
- 设置角度: `0x1200FD00 + motor_id` + `[0x16, 0x70, 0x00, 0x00] + angle_bytes`
- 启用电机: `0x0300FD00 + motor_id` + `[0x00]*8`
- 设置零位: `0x0600FD00 + motor_id` + `[0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00]`

## 📋 API接口

### 手部控制
- `POST /api/hand/` - 手部控制
- `POST /api/config/update` - 更新外部配置文件

### 机械臂控制  
- `GET /api/arms` - 获取机械臂列表
- `POST /api/arm/` - 机械臂控制
- `POST /api/joints/` - 关节控制

## 🎵 乐器配置说明

### 萨克斯 (SKS)
- 标准按压和松开位置
- 适用于萨克斯演奏

### 唢呐 (SN)  
- 基础按压和松开位置
- 高音拇指位置 (只修改拇指和拇指旋转)
- 倍高音拇指位置 (只修改拇指和拇指旋转)

## ⚠️ 注意事项

1. **文件权限**: 确保程序有权限写入配置文件
2. **CAN服务器**: 需要CAN桥接服务器运行在localhost:5260
3. **设备连接**: 确保机械臂和手部设备已正确连接
4. **备份配置**: 建议定期备份配置文件

## 🔄 工作流程

1. **调整位置**: 通过滑动条调整手指到理想位置
2. **测试效果**: 实时查看手部动作效果
3. **保存配置**: 点击保存按钮更新外部配置文件
4. **应用配置**: 其他程序可以读取更新后的配置文件

这样您就可以通过Web界面方便地调整和保存手部控制参数了！
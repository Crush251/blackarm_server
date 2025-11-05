package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v2"
)

//go:embed static
var staticFiles embed.FS

// Config 配置文件结构
type Config struct {
	Arms  map[string]ArmConfig     `yaml:"arms"`
	Hands map[string]HandConfigNew `yaml:"hands"`

	// CAN桥接URL
	CanBridgeURL string `yaml:"can_bridge_url"`

	// 手部预设配置 - 直接从配置文件读取
	SksLeftPressProfile    []int `yaml:"sks_left_press_profile"`
	SksLeftReleaseProfile  []int `yaml:"sks_left_release_profile"`
	SksRightPressProfile   []int `yaml:"sks_right_press_profile"`
	SksRightReleaseProfile []int `yaml:"sks_right_release_profile"`
	SnLeftPressProfile     []int `yaml:"sn_left_press_profile"`
	SnLeftReleaseProfile   []int `yaml:"sn_left_release_profile"`
	SnLeftHighThumb        []int `yaml:"sn_left_high_Thumb"`
	SnLeftHighProThumb     []int `yaml:"sn_left_high_pro_Thumb"`
	SnRightPressProfile    []int `yaml:"sn_right_press_profile"`
	SnRightReleaseProfile  []int `yaml:"sn_right_release_profile"`
	HandsLeft              []int `yaml:"handsleft"`
	HandsRight             []int `yaml:"handsright"`

	// 关节角度序列配置 - 注意：序列不在主配置文件中，使用单独的JSON文件
	JointSequences []JointSequence `yaml:"joint_sequences"`
}

// JointSequence 关节角度序列 - 使用JSON格式
type JointSequence struct {
	Name     string          `json:"name"`
	ArmType  string          `json:"arm_type"`  // "left" or "right"
	ArmModel string          `json:"arm_model"` // 暂定"old" or "new"
	Angles   []JointAngleSet `json:"angles"`
}

// JointAngleSet 一组关节角度值 - 使用JSON格式
type JointAngleSet struct {
	Name   string             `json:"name"`
	Values map[string]float32 `json:"values"` // motor_id -> angle
}

type ArmConfig struct {
	DeviceName string `yaml:"device_name"`
	ArmType    string `yaml:"arm_type"` // "left" or "right"
}

type HandConfig struct {
	DeviceID   int    `yaml:"device_id"`
	DeviceName string `yaml:"device_name"`
	HandType   string `yaml:"hand_type"` // "left" or "right"
}

// HandConfigNew 新的手部配置结构
type HandConfigNew struct {
	Interface string `yaml:"interface"`
	ID        string `yaml:"id"`
}

// ArmInfo 手臂信息
type ArmInfo struct {
	Interface  string `json:"interface"`
	DeviceName string `json:"device_name"`
	ArmType    string `json:"arm_type"` // "left" or "right"
	MotorIDs   []int  `json:"motor_ids"`
	Status     string `json:"status"`
}

// JointControl 关节控制参数
type JointControl struct {
	JointID  int     `json:"joint_id"`
	Angle    float32 `json:"angle"`
	Speed    float32 `json:"speed"`
	LocKp    float32 `json:"loc_kp"`
	SpeedKp  float32 `json:"speed_kp"`
	SpeedKi  float32 `json:"speed_ki"`
	FiltGain float32 `json:"filt_gain"`
}

// HandControl 手部控制参数
type HandControl struct {
	Thumb       int `json:"thumb"`
	ThumbRotate int `json:"thumb_rotate"`
	Index       int `json:"index"`
	Middle      int `json:"middle"`
	Ring        int `json:"ring"`
	Pinky       int `json:"pinky"`
}

// HandInfo 手部信息
type HandInfo struct {
	Interface  string `json:"interface"`
	DeviceID   int    `json:"device_id"`
	DeviceName string `json:"device_name"`
	HandType   string `json:"hand_type"` // "left" or "right"
	Status     string `json:"status"`
}

// ControlRequest 控制请求
type ControlRequest struct {
	Interface string         `json:"interface"`
	Action    string         `json:"action"`
	JointID   int            `json:"joint_id,omitempty"`
	Value     float32        `json:"value,omitempty"`
	Joints    []JointControl `json:"joints,omitempty"`
	Hand      HandControl    `json:"hand,omitempty"`
	Profile   string         `json:"profile,omitempty"`
	HandType  string         `json:"hand_type,omitempty"`
	MotorIDs  []int          `json:"motor_ids,omitempty"` // 用于设置零点时指定电机ID
}

// ControlResponse 控制响应
type ControlResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// WebServer Web服务器
type WebServer struct {
	config      *Config
	controllers map[string]*BlackArmController
	mutex       sync.RWMutex

	// 临时角度记录
	tempAngleRecords map[string][]JointAngleSet // interface -> angle sets
	tempMutex        sync.RWMutex

	// 当前角度状态 - 用于实时更新前端显示
	currentAngles map[string]map[string]float32 // interface -> motor_id -> angle
	anglesMutex   sync.RWMutex
}

// NewWebServer 创建Web服务器
func NewWebServer(configPath string) (*WebServer, error) {
	// 读取配置文件
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %v", err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %v", err)
	}

	server := &WebServer{
		config:           &config,
		controllers:      make(map[string]*BlackArmController),
		tempAngleRecords: make(map[string][]JointAngleSet),
		currentAngles:    make(map[string]map[string]float32),
	}

	// 加载序列配置文件
	err = server.loadSequenceConfig()
	if err != nil {
		log.Printf("加载序列配置失败: %v", err)
		// 不返回错误，继续启动服务器
	}

	// 不再进行自动检测，直接使用配置文件中的设置
	log.Printf("使用配置文件中的设备配置，跳过自动检测")

	// 初始化所有手臂控制器
	for interfaceName, armConfig := range config.Arms {
		controller := NewBlackArmController("http://localhost:5260", interfaceName, armConfig.DeviceName)
		if controller != nil {
			server.controllers[interfaceName] = controller
			log.Printf("初始化手臂控制器: %s (%s)", interfaceName, armConfig.DeviceName)
		}
	}

	return server, nil
}

// determineArmType 根据电机ID范围判断臂类型
func determineArmType(motorIDs []int) string {
	if len(motorIDs) == 0 {
		return "unknown"
	}

	// 检查是否所有电机ID都在51-57范围内（右臂）
	allInRightRange := true
	for _, id := range motorIDs {
		if id < 51 || id > 57 {
			allInRightRange = false
			break
		}
	}
	if allInRightRange {
		return "right"
	}

	// 检查是否所有电机ID都在61-67范围内（左臂）
	allInLeftRange := true
	for _, id := range motorIDs {
		if id < 61 || id > 67 {
			allInLeftRange = false
			break
		}
	}
	if allInLeftRange {
		return "left"
	}

	// 如果不在预期范围内，返回unknown
	return "unknown"
}

// ensureJSONDir 确保json目录存在
func ensureJSONDir(dirPath string) error {
	// 使用os.MkdirAll确保目录存在
	err := ioutil.WriteFile(dirPath+"/.gitkeep", []byte(""), 0644)
	return err
}

// loadSequenceConfig 加载序列配置文件
func (ws *WebServer) loadSequenceConfig() error {
	sequenceDirPath := "json"

	// 确保json目录存在
	if err := ensureJSONDir(sequenceDirPath); err != nil {
		log.Printf("创建json目录失败: %v", err)
	}

	// 读取json目录下的所有文件
	files, err := ioutil.ReadDir(sequenceDirPath)
	if err != nil {
		log.Printf("读取json目录失败: %v，跳过序列加载", err)
		ws.config.JointSequences = []JointSequence{}
		return nil
	}

	// 遍历所有JSON文件并加载
	var allSequences []JointSequence
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		filePath := sequenceDirPath + "/" + file.Name()
		data, err := ioutil.ReadFile(filePath)
		if err != nil {
			log.Printf("读取序列文件失败: %s, %v", filePath, err)
			continue
		}

		var sequence JointSequence
		err = json.Unmarshal(data, &sequence)
		if err != nil {
			log.Printf("解析序列文件失败: %s, %v", filePath, err)
			continue
		}

		allSequences = append(allSequences, sequence)
		log.Printf("加载序列: %s (%s臂, %d 组角度) 从文件 %s", sequence.Name, sequence.ArmType, len(sequence.Angles), file.Name())
	}

	// 加载到内存配置中
	ws.config.JointSequences = allSequences
	log.Printf("成功加载 %d 个角度序列", len(allSequences))

	return nil
}

// Start 启动Web服务器
func (ws *WebServer) Start(port int) error {
	// 静态文件服务 - 使用嵌入的静态文件
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("创建静态文件系统失败: %v", err)
	}

	// API路由 - 必须在静态文件服务器之前注册
	http.HandleFunc("/api/arms", ws.getArmsHandler)
	http.HandleFunc("/api/hands", ws.getHandsHandler)
	http.HandleFunc("/api/arm/", ws.armControlHandler)
	http.HandleFunc("/api/hand/", ws.handControlHandler)
	http.HandleFunc("/api/joints/", ws.jointControlHandler)
	http.HandleFunc("/api/config/update", ws.updateConfigHandler)

	// 新增：关节角度序列管理
	http.HandleFunc("/api/joint-sequences/", ws.jointSequenceHandler)
	http.HandleFunc("/api/joint-sequences/temp/", ws.tempAngleHandler)
	http.HandleFunc("/api/joint-sequences/execute/", ws.executeSequenceHandler)
	http.HandleFunc("/api/joint-sequences/merge/", ws.mergeSequencesHandler)
	http.HandleFunc("/api/joint-sequences/merged/", ws.listMergedSequencesHandler)
	http.HandleFunc("/api/joint-sequences/execute-merged/", ws.executeMergedSequenceHandler)
	http.HandleFunc("/api/current-angles/", ws.getCurrentAnglesHandler)

	// 静态文件服务器 - 必须在最后注册，作为默认处理
	http.Handle("/", http.FileServer(http.FS(staticFS)))

	log.Printf("Web服务器启动在端口 %d", port)
	log.Printf("已注册API路由: /api/arms, /api/hands, /api/arm/, /api/hand/, /api/joints/, /api/config/update")
	fmt.Println("🌐 访问地址: http://localhost:8080")
	return http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
}

// getArmsHandler 获取所有手臂信息
func (ws *WebServer) getArmsHandler(w http.ResponseWriter, r *http.Request) {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()

	var arms []ArmInfo
	for interfaceName, controller := range ws.controllers {
		motorIDs := controller.GetMotorIDs()
		armType := determineArmType(motorIDs)
		deviceName := ws.config.Arms[interfaceName].DeviceName
		arm := ArmInfo{
			Interface:  interfaceName,
			DeviceName: deviceName,
			ArmType:    armType,
			MotorIDs:   motorIDs,
			Status:     "connected",
		}
		arms = append(arms, arm)

		log.Printf("检测到机械臂: %s - %s (%s), 电机ID: %v", interfaceName, deviceName, armType, motorIDs)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(arms)
}

// getHandsHandler 获取所有手部信息
func (ws *WebServer) getHandsHandler(w http.ResponseWriter, r *http.Request) {
	ws.mutex.RLock()
	defer ws.mutex.RUnlock()

	var hands []HandInfo
	for handSide, handConfig := range ws.config.Hands {
		// 解析设备ID
		var deviceID int
		if handConfig.ID != "" {
			if strings.HasPrefix(handConfig.ID, "0x") {
				if id, err := strconv.ParseInt(handConfig.ID[2:], 16, 32); err == nil {
					deviceID = int(id)
				}
			}
		}

		// 根据handSide确定设备名称
		deviceName := fmt.Sprintf("L6_%s", handSide)

		hand := HandInfo{
			Interface:  handConfig.Interface,
			DeviceID:   deviceID,
			DeviceName: deviceName,
			HandType:   handSide,
			Status:     "connected",
		}
		hands = append(hands, hand)

		log.Printf("返回手部设备: %s - %s (%s手, ID: %d)", handConfig.Interface, deviceName, handSide, deviceID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(hands)
}

// armControlHandler 手臂控制处理
func (ws *WebServer) armControlHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "只支持POST方法", http.StatusMethodNotAllowed)
		return
	}

	var req ControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "解析请求失败", http.StatusBadRequest)
		return
	}

	log.Printf("收到手臂控制请求: interface=%s, action=%s", req.Interface, req.Action)

	ws.mutex.RLock()
	controller, exists := ws.controllers[req.Interface]
	ws.mutex.RUnlock()

	if !exists {
		http.Error(w, "未找到指定的手臂接口", http.StatusNotFound)
		return
	}

	var response ControlResponse

	switch req.Action {
	case "enable":
		err := controller.EnableMotor("全部关节")
		response.Success = err == nil
		if err != nil {
			response.Message = fmt.Sprintf("启用失败: %v", err)
		} else {
			response.Message = "启用成功"
		}

	case "disable":
		err := controller.DisableMotor()
		response.Success = err == nil
		if err != nil {
			response.Message = fmt.Sprintf("禁用失败: %v", err)
		} else {
			response.Message = "禁用成功"
		}

	case "set_zero":
		// 检查是否有指定电机ID列表
		motorIDs := req.MotorIDs
		if len(motorIDs) == 0 {
			motorIDs = nil // 设置为nil表示所有电机
		}

		err := controller.SetMotorZeroByIDs(motorIDs)
		response.Success = err == nil
		if err != nil {
			response.Message = fmt.Sprintf("设置零位失败: %v", err)
		} else {
			if len(motorIDs) > 0 {
				response.Message = fmt.Sprintf("设置电机 %v 零位成功", motorIDs)
			} else {
				response.Message = "设置所有电机零位成功"
			}
		}

	case "return_zero":
		err := controller.ReturnZero()
		response.Success = err == nil
		if err != nil {
			response.Message = fmt.Sprintf("回零失败: %v", err)
		} else {
			response.Message = "回零成功"
		}

	case "clean_error":
		err := controller.CleanError()
		response.Success = err == nil
		if err != nil {
			response.Message = fmt.Sprintf("清除错误失败: %v", err)
		} else {
			response.Message = "清除错误成功"
		}

	case "queryangles":
		log.Printf("收到查询角度请求: interface=%s", req.Interface)

		// 获取CAN桥接URL
		canBridgeURL := ws.config.CanBridgeURL
		if canBridgeURL == "" {
			canBridgeURL = "http://localhost:5260"
		}

		log.Printf("开始查询角度: interface=%s, canBridgeURL=%s", req.Interface, canBridgeURL)

		// 调用查询函数
		angles, params, err := QueryCurrentAngles(canBridgeURL, req.Interface, controller.GetMotorIDs())

		if err != nil {
			log.Printf("查询失败: %v", err)
			response.Success = false
			response.Message = fmt.Sprintf("查询失败: %v", err)
		} else {
			log.Printf("查询成功: angles=%v, params=%v", angles, params)
			response.Success = true
			response.Message = "查询成功"
			response.Data = map[string]interface{}{
				"angles": angles,
				"params": params,
			}
		}

	default:
		log.Printf("不支持的操作: action='%s' (长度=%d)", req.Action, len(req.Action))
		response.Success = false
		response.Message = fmt.Sprintf("不支持的操作: %s", req.Action)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handControlHandler 手部控制处理
func (ws *WebServer) handControlHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "只支持POST方法", http.StatusMethodNotAllowed)
		return
	}

	var req ControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "解析请求失败", http.StatusBadRequest)
		return
	}

	ws.mutex.RLock()
	var deviceID int
	var exists bool

	// 查找对应的手部配置
	for _, config := range ws.config.Hands {
		if config.Interface == req.Interface {
			exists = true
			// 解析设备ID
			if strings.HasPrefix(config.ID, "0x") {
				if id, err := strconv.ParseInt(config.ID[2:], 16, 32); err == nil {
					deviceID = int(id)
				}
			}
			break
		}
	}
	ws.mutex.RUnlock()

	if !exists {
		http.Error(w, "未找到指定的手部接口", http.StatusNotFound)
		return
	}

	var response ControlResponse

	switch req.Action {
	case "set_fingers":
		err := ws.sendHandCommand(req.Interface, deviceID, req.Hand)
		response.Success = err == nil
		if err != nil {
			response.Message = fmt.Sprintf("设置手指失败: %v", err)
		} else {
			response.Message = "设置手指成功"
		}

	case "set_profile":
		profileData, err := ws.setHandProfile(req.Interface, deviceID, req.HandType, req.Profile)
		response.Success = err == nil
		if err != nil {
			response.Message = fmt.Sprintf("设置预设失败: %v", err)
		} else {
			response.Message = "设置预设成功"
			// 返回实际设置的值，供前端更新滑动条
			response.Data = map[string]interface{}{
				"profile_values": profileData,
			}
		}

	default:
		response.Success = false
		response.Message = "不支持的操作"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// sendHandCommand 发送手部控制命令
func (ws *WebServer) sendHandCommand(interfaceName string, deviceID int, hand HandControl) error {
	// 构建CAN消息数据
	data := []byte{0x01} // 控制码固定为0x01
	data = append(data, byte(hand.Thumb))
	data = append(data, byte(hand.ThumbRotate))
	data = append(data, byte(hand.Index))
	data = append(data, byte(hand.Middle))
	data = append(data, byte(hand.Ring))
	data = append(data, byte(hand.Pinky))

	// // 确保数据长度为8字节
	// for len(data) < 8 {
	// 	data = append(data, 0x00)
	// }

	// 构建CAN消息
	canMessage := map[string]interface{}{
		"interface": interfaceName,
		"id":        deviceID,
		"data":      data,
	}

	log.Printf("发送手部控制命令: interface=%s, id=%d, data=%v", interfaceName, deviceID, data)

	// 发送到CAN桥接服务器
	jsonData, err := json.Marshal(canMessage)
	if err != nil {
		return fmt.Errorf("序列化CAN消息失败: %v", err)
	}

	resp, err := http.Post("http://localhost:5260/api/can", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("发送CAN消息失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("CAN消息发送失败，状态码: %d", resp.StatusCode)
	}

	log.Printf("手部控制命令发送成功")
	return nil
}

// setHandProfile 设置手部预设位置
func (ws *WebServer) setHandProfile(interfaceName string, deviceID int, handType, profile string) ([]int, error) {
	ws.mutex.RLock()
	config := ws.config
	ws.mutex.RUnlock()

	var profileData []int

	// 根据手部类型、左右手和预设类型选择数据
	switch {
	case handType == "sks" && deviceID == 40 && profile == "press":
		profileData = config.SksLeftPressProfile
	case handType == "sks" && deviceID == 40 && profile == "release":
		profileData = config.SksLeftReleaseProfile
	case handType == "sks" && deviceID == 39 && profile == "press":
		profileData = config.SksRightPressProfile
	case handType == "sks" && deviceID == 39 && profile == "release":
		profileData = config.SksRightReleaseProfile
	case handType == "sn" && deviceID == 40 && profile == "press":
		profileData = config.SnLeftPressProfile
	case handType == "sn" && deviceID == 40 && profile == "release":
		profileData = config.SnLeftReleaseProfile
	case handType == "sn" && deviceID == 40 && profile == "high_thumb":
		if len(config.SnLeftHighThumb) > 0 {
			// 只更新拇指和拇指旋转
			profileData = make([]int, 6)
			copy(profileData, config.SnLeftPressProfile)
			if len(config.SnLeftHighThumb) >= 2 {
				profileData[0] = config.SnLeftHighThumb[0]
				profileData[1] = config.SnLeftHighThumb[1]
			}
		} else {
			return nil, fmt.Errorf("该手部类型不支持高音拇指预设")
		}
	case handType == "sn" && deviceID == 40 && profile == "high_pro_thumb":
		if len(config.SnLeftHighProThumb) > 0 {
			// 只更新拇指和拇指旋转
			profileData = make([]int, 6)
			copy(profileData, config.SnLeftPressProfile)
			if len(config.SnLeftHighProThumb) >= 2 {
				profileData[0] = config.SnLeftHighProThumb[0]
				profileData[1] = config.SnLeftHighProThumb[1]
			}
		} else {
			return nil, fmt.Errorf("该手部类型不支持倍高音拇指预设")
		}
	case handType == "sn" && deviceID == 39 && profile == "press":
		profileData = config.SnRightPressProfile
	case handType == "sn" && deviceID == 39 && profile == "release":
		profileData = config.SnRightReleaseProfile
	default:
		return nil, fmt.Errorf("不支持的配置组合: %s, deviceID=%d, profile=%s", handType, deviceID, profile)
	}

	if len(profileData) != 6 {
		return nil, fmt.Errorf("预设数据长度不正确，期望6个值，实际%d个", len(profileData))
	}

	// 构建手部控制参数
	hand := HandControl{
		Thumb:       profileData[0],
		ThumbRotate: profileData[1],
		Index:       profileData[2],
		Middle:      profileData[3],
		Ring:        profileData[4],
		Pinky:       profileData[5],
	}

	err := ws.sendHandCommand(interfaceName, deviceID, hand)
	if err != nil {
		return nil, err
	}

	// 返回实际使用的配置值
	return profileData, nil
}

// updateConfigHandler 更新外部配置文件
func (ws *WebServer) updateConfigHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "只支持POST方法", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		HandType string `json:"hand_type"`
		Profile  string `json:"profile"`
		Values   []int  `json:"values"`
		Hand     string `json:"hand"` // "left" or "right"
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "解析请求失败", http.StatusBadRequest)
		return
	}

	var response ControlResponse

	err := ws.updateExternalConfig(req.HandType, req.Profile, req.Values, req.Hand)
	response.Success = err == nil
	if err != nil {
		response.Message = fmt.Sprintf("更新配置文件失败: %v", err)
	} else {
		response.Message = "更新配置文件成功"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// updateExternalConfig 更新外部配置文件
func (ws *WebServer) updateExternalConfig(handType, profile string, values []int, hand string) error {
	// 支持同级目录和目标目录
	relConfigPath := "config.yaml"                               // 默认同级目录
	targetConfigPath := "/home/linkerhand/sks/sksgo/config.yaml" // 绝对目标目录

	// 构建配置键名
	var configKey string
	switch {
	case handType == "sks" && hand == "left" && profile == "press":
		configKey = "sks_left_press_profile"
	case handType == "sks" && hand == "left" && profile == "release":
		configKey = "sks_left_release_profile"
	case handType == "sks" && hand == "right" && profile == "press":
		configKey = "sks_right_press_profile"
	case handType == "sks" && hand == "right" && profile == "release":
		configKey = "sks_right_release_profile"
	case handType == "sn" && hand == "left" && profile == "press":
		configKey = "sn_left_press_profile"
	case handType == "sn" && hand == "left" && profile == "release":
		configKey = "sn_left_release_profile"
	case handType == "sn" && hand == "left" && profile == "high_thumb":
		configKey = "sn_left_high_Thumb"
	case handType == "sn" && hand == "left" && profile == "high_pro_thumb":
		configKey = "sn_left_high_pro_Thumb"
	case handType == "sn" && hand == "right" && profile == "press":
		configKey = "sn_right_press_profile"
	case handType == "sn" && hand == "right" && profile == "release":
		configKey = "sn_right_release_profile"
	default:
		return fmt.Errorf("不支持的配置组合: %s_%s_%s", handType, hand, profile)
	}

	// 先更新同级目录下的config.yaml
	err := ws.updateYAMLField(relConfigPath, configKey, values)
	if err != nil {
		return fmt.Errorf("更新同级目录下YAML字段失败: %v", err)
	}

	// 再更新目标目录下的config.yaml（如果和同级目录不同）
	if relConfigPath != targetConfigPath {
		err2 := ws.updateYAMLField(targetConfigPath, configKey, values)
		if err2 != nil {
			// 若目标目录失败，则警告但不阻止主进程
			log.Printf("警告: 更新目标目录YAML字段失败: %v", err2)
		} else {
			log.Printf("成功同步目标目录配置文件 %s: %s = %v", targetConfigPath, configKey, values)
		}
	}

	// 如果是保存press类型，自动计算并保存release值
	if profile == "press" && len(values) >= 6 {
		releaseValues := make([]int, 6)
		copy(releaseValues, values)

		if handType == "sn" {
			// SN: 食指(2)、中指(3)、无名指(4) +20
			for i := 2; i <= 4; i++ {
				if releaseValues[i]+20 > 255 {
					releaseValues[i] = 255
				} else {
					releaseValues[i] += 20
				}
			}
		} else if handType == "sks" {
			// SKS: 食指(2)、中指(3)、无名指(4)、小指(5) +20
			for i := 2; i <= 5; i++ {
				if releaseValues[i]+20 > 255 {
					releaseValues[i] = 255
				} else {
					releaseValues[i] += 20
				}
			}
		}

		// 构建release配置键名
		var releaseConfigKey string
		switch {
		case handType == "sks" && hand == "left":
			releaseConfigKey = "sks_left_release_profile"
		case handType == "sks" && hand == "right":
			releaseConfigKey = "sks_right_release_profile"
		case handType == "sn" && hand == "left":
			releaseConfigKey = "sn_left_release_profile"
		case handType == "sn" && hand == "right":
			releaseConfigKey = "sn_right_release_profile"
		}

		if releaseConfigKey != "" {
			// 更新release配置
			err := ws.updateYAMLField(relConfigPath, releaseConfigKey, releaseValues)
			if err != nil {
				log.Printf("警告: 自动更新release配置失败: %v", err)
			} else {
				log.Printf("自动计算并保存release配置: %s = %v", releaseConfigKey, releaseValues)
			}

			// 同步到目标目录
			if relConfigPath != targetConfigPath {
				err2 := ws.updateYAMLField(targetConfigPath, releaseConfigKey, releaseValues)
				if err2 != nil {
					log.Printf("警告: 同步目标目录release配置失败: %v", err2)
				}
			}
		}
	}

	// 重新加载同级目录下的config.yaml以更新内存中的配置
	err = ws.reloadConfig()
	if err != nil {
		log.Printf("重新加载配置失败: %v", err)
		// 不返回错误，继续执行
	}

	log.Printf("成功更新配置文件(同级和目标): [%s] [%s] 字段 %s = %v", relConfigPath, targetConfigPath, configKey, values)
	return nil
}

// reloadConfig 重新加载配置文件
func (ws *WebServer) reloadConfig() error {
	// 读取配置文件
	data, err := ioutil.ReadFile("config.yaml")
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %v", err)
	}

	var newConfig Config
	err = yaml.Unmarshal(data, &newConfig)
	if err != nil {
		return fmt.Errorf("解析配置文件失败: %v", err)
	}

	// 更新配置（保留原有的关节序列配置）
	ws.mutex.Lock()
	oldSequences := ws.config.JointSequences
	ws.config = &newConfig
	ws.config.JointSequences = oldSequences
	ws.mutex.Unlock()

	log.Printf("配置文件重新加载成功")
	return nil
}

// updateYAMLField 更新YAML文件中的特定字段，保留格式和注释
func (ws *WebServer) updateYAMLField(filePath, key string, values []int) error {
	// 读取原始文件内容
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return err
	}

	content := string(data)

	// 构建新的值字符串
	var valuesStr string
	if len(values) > 0 {
		valuesStr = "["
		for i, v := range values {
			if i > 0 {
				valuesStr += ", "
			}
			valuesStr += fmt.Sprintf("%d", v)
		}
		valuesStr += "]"
	} else {
		valuesStr = "[]"
	}

	// 使用正则表达式查找并替换指定的配置行
	// 匹配模式：key: [数字, 数字, ...] 可能跟着注释
	pattern := fmt.Sprintf(`(?m)^(\s*)%s:\s*\[[^\]]*\](.*)$`, regexp.QuoteMeta(key))

	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("编译正则表达式失败: %v", err)
	}

	// 检查是否找到匹配项
	if !re.MatchString(content) {
		return fmt.Errorf("在配置文件中未找到配置项: %s", key)
	}

	// 替换配置值，保留缩进和注释
	newContent := re.ReplaceAllString(content, fmt.Sprintf("${1}%s: %s${2}", key, valuesStr))

	// 写回文件
	err = ioutil.WriteFile(filePath, []byte(newContent), 0644)
	if err != nil {
		return fmt.Errorf("写入文件失败: %v", err)
	}

	return nil
}

// jointControlHandler 关节控制处理
func (ws *WebServer) jointControlHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "只支持POST方法", http.StatusMethodNotAllowed)
		return
	}

	var req ControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "解析请求失败", http.StatusBadRequest)
		return
	}

	ws.mutex.RLock()
	controller, exists := ws.controllers[req.Interface]
	ws.mutex.RUnlock()

	if !exists {
		http.Error(w, "未找到指定的手臂接口", http.StatusNotFound)
		return
	}

	var response ControlResponse

	switch req.Action {
	case "set_angle":
		err := controller.SetAngle(req.JointID, req.Value)
		response.Success = err == nil
		if err != nil {
			response.Message = fmt.Sprintf("设置角度失败: %v", err)
		} else {
			response.Message = "设置角度成功"
			// 更新当前角度状态
			ws.updateCurrentAngle(req.Interface, strconv.Itoa(req.JointID), req.Value)
		}

	case "set_speed":
		err := controller.SetSpeed(req.JointID, req.Value)
		response.Success = err == nil
		if err != nil {
			response.Message = fmt.Sprintf("设置速度失败: %v", err)
		} else {
			response.Message = "设置速度成功"
		}

	case "set_loc_kp":
		err := controller.SetMotorLocKp(req.JointID, req.Value)
		response.Success = err == nil
		if err != nil {
			response.Message = fmt.Sprintf("设置位置Kp失败: %v", err)
		} else {
			response.Message = "设置位置Kp成功"
		}

	case "set_speed_kp":
		err := controller.SetMotorSpeedKp(req.JointID, req.Value)
		response.Success = err == nil
		if err != nil {
			response.Message = fmt.Sprintf("设置速度Kp失败: %v", err)
		} else {
			response.Message = "设置速度Kp成功"
		}

	case "set_speed_ki":
		err := controller.SetMotorSpeedKi(req.JointID, req.Value)
		response.Success = err == nil
		if err != nil {
			response.Message = fmt.Sprintf("设置速度Ki失败: %v", err)
		} else {
			response.Message = "设置速度Ki成功"
		}

	case "set_filt_gain":
		err := controller.SetMotorSpeedFiltGain(req.JointID, req.Value)
		response.Success = err == nil
		if err != nil {
			response.Message = fmt.Sprintf("设置滤波增益失败: %v", err)
		} else {
			response.Message = "设置滤波增益成功"
		}

	case "set_all_angles":
		var angles []float32
		for _, joint := range req.Joints {
			angles = append(angles, joint.Angle)
		}
		err := controller.SetAngles(angles)
		response.Success = err == nil
		if err != nil {
			response.Message = fmt.Sprintf("设置所有角度失败: %v", err)
		} else {
			response.Message = "设置所有角度成功"
			// 更新当前角度状态
			for _, joint := range req.Joints {
				ws.updateCurrentAngle(req.Interface, strconv.Itoa(joint.JointID), joint.Angle)
			}
		}
	case "set_down_up_angles":
		//支持启动我们保存的序列，前端勾选，将json传给后端启动
		//按照json的left和right,分别并行执行

	default:
		response.Success = false
		response.Message = "不支持的操作"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// tempAngleHandler 临时角度记录处理
func (ws *WebServer) tempAngleHandler(w http.ResponseWriter, r *http.Request) {
	var response ControlResponse

	switch r.Method {
	case "POST":
		// 记录当前角度
		var req struct {
			Interface string             `json:"interface"`
			Name      string             `json:"name"`
			Angles    map[string]float32 `json:"angles"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "解析请求失败", http.StatusBadRequest)
			return
		}

		ws.mutex.RLock()
		_, exists := ws.controllers[req.Interface]
		ws.mutex.RUnlock()

		if !exists {
			response.Success = false
			response.Message = "未找到指定的机械臂接口"
		} else {
			// 使用从前端传来的角度数据
			angleSet := JointAngleSet{
				Name:   req.Name,
				Values: req.Angles,
			}

			ws.tempMutex.Lock()
			if ws.tempAngleRecords[req.Interface] == nil {
				ws.tempAngleRecords[req.Interface] = make([]JointAngleSet, 0)
			}
			ws.tempAngleRecords[req.Interface] = append(ws.tempAngleRecords[req.Interface], angleSet)
			ws.tempMutex.Unlock()

			response.Success = true
			response.Message = fmt.Sprintf("已记录角度组: %s", req.Name)
			response.Data = angleSet
		}

	case "GET":
		// 获取临时记录
		interfaceName := r.URL.Query().Get("interface")

		ws.tempMutex.RLock()
		records := ws.tempAngleRecords[interfaceName]
		ws.tempMutex.RUnlock()

		response.Success = true
		response.Message = "获取临时记录成功"
		response.Data = records

	case "DELETE":
		// 清除临时记录
		interfaceName := r.URL.Query().Get("interface")

		ws.tempMutex.Lock()
		delete(ws.tempAngleRecords, interfaceName)
		ws.tempMutex.Unlock()

		response.Success = true
		response.Message = "已清除临时记录"

	default:
		http.Error(w, "不支持的HTTP方法", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// jointSequenceHandler 关节序列管理处理
func (ws *WebServer) jointSequenceHandler(w http.ResponseWriter, r *http.Request) {
	var response ControlResponse

	switch r.Method {
	case "POST":
		// 保存序列到配置文件
		var req struct {
			Interface string `json:"interface"`
			Name      string `json:"name"`
			ArmModel  string `json:"arm_model"` // "old" or "new"
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "解析请求失败", http.StatusBadRequest)
			return
		}

		ws.tempMutex.RLock()
		tempRecords := ws.tempAngleRecords[req.Interface]
		ws.tempMutex.RUnlock()

		if len(tempRecords) == 0 {
			response.Success = false
			response.Message = "没有临时记录可保存"
		} else {
			// 根据接口获取臂类型
			var armType string
			ws.mutex.RLock()
			if controller, exists := ws.controllers[req.Interface]; exists {
				motorIDs := controller.GetMotorIDs()
				armType = determineArmType(motorIDs)
			}
			ws.mutex.RUnlock()

			// 如果没有指定 arm_model，默认为 "old"
			armModel := req.ArmModel
			if armModel == "" {
				armModel = "old"
			}

			sequence := JointSequence{
				Name:     req.Name,
				ArmType:  armType,
				ArmModel: armModel,
				Angles:   make([]JointAngleSet, len(tempRecords)),
			}
			copy(sequence.Angles, tempRecords)

			err := ws.saveJointSequence(sequence)
			if err != nil {
				response.Success = false
				response.Message = fmt.Sprintf("保存序列失败: %v", err)
			} else {
				response.Success = true
				response.Message = "序列保存成功"

				// 清除临时记录
				ws.tempMutex.Lock()
				delete(ws.tempAngleRecords, req.Interface)
				ws.tempMutex.Unlock()
			}
		}

	case "GET":
		// 重新加载序列配置文件
		err := ws.loadSequenceConfig()
		if err != nil {
			log.Printf("重新加载序列配置失败: %v", err)
		}

		// 获取所有序列
		ws.mutex.RLock()
		sequences := ws.config.JointSequences
		ws.mutex.RUnlock()

		response.Success = true
		response.Message = "获取序列成功"
		response.Data = sequences

	case "DELETE":
		// 删除序列
		var req struct {
			SequenceName string `json:"sequence_name"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "解析请求失败", http.StatusBadRequest)
			return
		}

		err := ws.deleteJointSequence(req.SequenceName)
		if err != nil {
			response.Success = false
			response.Message = fmt.Sprintf("删除序列失败: %v", err)
		} else {
			response.Success = true
			response.Message = "序列删除成功"
		}

	default:
		http.Error(w, "不支持的HTTP方法", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// executeSequenceHandler 执行序列处理
func (ws *WebServer) executeSequenceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "只支持POST方法", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SequenceName string `json:"sequence_name"`
		Interface    string `json:"interface"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "解析请求失败", http.StatusBadRequest)
		return
	}

	var response ControlResponse

	ws.mutex.RLock()
	controller, exists := ws.controllers[req.Interface]
	if !exists {
		ws.mutex.RUnlock()
		response.Success = false
		response.Message = "未找到指定的机械臂接口"
	} else {
		// 获取当前接口的臂类型
		motorIDs := controller.GetMotorIDs()
		currentArmType := determineArmType(motorIDs)

		// 查找序列 - 通过name和arm_type匹配
		var sequence *JointSequence
		for _, seq := range ws.config.JointSequences {
			if seq.Name == req.SequenceName && seq.ArmType == currentArmType {
				sequence = &seq
				break
			}
		}
		ws.mutex.RUnlock()

		if sequence == nil {
			response.Success = false
			response.Message = "未找到指定的序列"
		} else {
			// 执行序列
			go ws.executeSequenceAsync(controller, sequence)
			response.Success = true
			response.Message = fmt.Sprintf("开始执行序列: %s", sequence.Name)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// executeSequenceAsync 异步执行序列
func (ws *WebServer) executeSequenceAsync(controller *BlackArmController, sequence *JointSequence) {
	log.Printf("开始执行序列: %s", sequence.Name)

	// 确定接口名称
	var interfaceName string
	ws.mutex.RLock()
	for iface, ctrl := range ws.controllers {
		if ctrl == controller {
			interfaceName = iface
			break
		}
	}
	ws.mutex.RUnlock()

	for i, angleSet := range sequence.Angles {
		log.Printf("执行第 %d 组角度: %s", i+1, angleSet.Name)

		// 设置每个关节的角度
		for motorIDStr, angle := range angleSet.Values {
			motorID, err := strconv.Atoi(motorIDStr)
			if err != nil {
				log.Printf("无效的电机ID: %s", motorIDStr)
				continue
			}

			err = controller.SetAngle(motorID, angle)
			if err != nil {
				log.Printf("设置电机 %d 角度失败: %v", motorID, err)
			} else {
				// 更新当前角度状态
				ws.updateCurrentAngle(interfaceName, motorIDStr, angle)
			}
		}

		time.Sleep(1 * time.Second)

	}

	log.Printf("序列执行完成: %s", sequence.Name)
}

// updateCurrentAngle 更新当前角度状态
func (ws *WebServer) updateCurrentAngle(interfaceName, motorID string, angle float32) {
	ws.anglesMutex.Lock()
	defer ws.anglesMutex.Unlock()

	if ws.currentAngles[interfaceName] == nil {
		ws.currentAngles[interfaceName] = make(map[string]float32)
	}
	ws.currentAngles[interfaceName][motorID] = angle
}

// getCurrentAnglesHandler 获取当前角度状态
func (ws *WebServer) getCurrentAnglesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "只支持GET方法", http.StatusMethodNotAllowed)
		return
	}

	interfaceName := r.URL.Query().Get("interface")
	if interfaceName == "" {
		http.Error(w, "缺少interface参数", http.StatusBadRequest)
		return
	}

	ws.anglesMutex.RLock()
	angles := ws.currentAngles[interfaceName]
	ws.anglesMutex.RUnlock()

	response := ControlResponse{
		Success: true,
		Message: "获取当前角度成功",
		Data:    angles,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// mergeSequencesHandler 合并两个序列处理
func (ws *WebServer) mergeSequencesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "只支持POST方法", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Sequence1Name string `json:"sequence1_name"`
		Sequence2Name string `json:"sequence2_name"`
		MergedName    string `json:"merged_name"`
		ArmModel      string `json:"arm_model"` // "old" or "new"
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "解析请求失败", http.StatusBadRequest)
		return
	}

	var response ControlResponse

	// 查找两个序列
	ws.mutex.RLock()
	var sequence1, sequence2 *JointSequence
	for i := range ws.config.JointSequences {
		if ws.config.JointSequences[i].Name == req.Sequence1Name {
			sequence1 = &ws.config.JointSequences[i]
		}
		if ws.config.JointSequences[i].Name == req.Sequence2Name {
			sequence2 = &ws.config.JointSequences[i]
		}
	}
	ws.mutex.RUnlock()

	if sequence1 == nil || sequence2 == nil {
		response.Success = false
		response.Message = "未找到指定的序列"
	} else {
		// 创建合并后的序列数组,保留两个独立的序列
		leftSeq := *sequence1
		rightSeq := *sequence2

		// 设置 arm_model，优先使用请求中的值，否则使用序列原有值，最后默认 "old"
		armModel := req.ArmModel
		if armModel == "" {
			armModel = leftSeq.ArmModel
		}
		if armModel == "" {
			armModel = "old"
		}

		// 更新两个序列的 arm_model
		leftSeq.ArmModel = armModel
		rightSeq.ArmModel = armModel

		// 判断是up还是down类型的合并
		isUpMerge := strings.Contains(strings.ToLower(req.MergedName), "up")
		isDownMerge := strings.Contains(strings.ToLower(req.MergedName), "down")

		// 区分新老臂（关节运动是反的）
		Angle2set62 := float32(0.1)
		Angle2set52 := float32(-0.1)
		if armModel == "new" {
			Angle2set62 = -Angle2set62
			Angle2set52 = -Angle2set52
		}

		// 如果是up合并,在第一段前添加初始角度，并同时生成down序列
		if isUpMerge {
			// 左臂添加初始角度 (电机ID 61-67)
			leftInitAngle := JointAngleSet{
				Name: "初始角度",
				Values: map[string]float32{
					"61": 0, "62": Angle2set62, "63": 0, "64": 0, "65": 0, "66": 0, "67": 0,
				},
			}
			leftSeq.Angles = append([]JointAngleSet{leftInitAngle}, leftSeq.Angles...)

			// 右臂添加初始角度 (电机ID 51-57)
			rightInitAngle := JointAngleSet{
				Name: "初始角度",
				Values: map[string]float32{
					"51": 0, "52": Angle2set52, "53": 0, "54": 0, "55": 0, "56": 0, "57": 0,
				},
			}
			rightSeq.Angles = append([]JointAngleSet{rightInitAngle}, rightSeq.Angles...)

			// 保存 UP 序列
			mergedSequences := []JointSequence{leftSeq, rightSeq}
			err := ws.saveMergedSequence(req.MergedName, mergedSequences)
			if err != nil {
				response.Success = false
				response.Message = fmt.Sprintf("保存UP序列失败: %v", err)
			} else {
				// 生成对应的 DOWN 序列
				downName := strings.Replace(req.MergedName, "up", "down", -1)
				downName = strings.Replace(downName, "Up", "down", -1)
				downName = strings.Replace(downName, "UP", "DOWN", -1)

				// 创建 DOWN 序列的副本
				leftSeqDown := leftSeq
				rightSeqDown := rightSeq

				// 更新序列名称
				leftSeqDown.Name = strings.Replace(leftSeq.Name, "up", "down", -1)
				rightSeqDown.Name = strings.Replace(rightSeq.Name, "up", "down", -1)

				// 反转左臂序列：去掉最后一个，然后反转
				if len(leftSeqDown.Angles) > 1 {
					leftAnglesWithoutLast := leftSeqDown.Angles[:len(leftSeqDown.Angles)-1]
					leftReversed := make([]JointAngleSet, len(leftAnglesWithoutLast))
					for i, angle := range leftAnglesWithoutLast {
						leftReversed[len(leftAnglesWithoutLast)-1-i] = angle
					}
					leftSeqDown.Angles = leftReversed
				}

				// 反转右臂序列：去掉最后一个，然后反转
				if len(rightSeqDown.Angles) > 1 {
					rightAnglesWithoutLast := rightSeqDown.Angles[:len(rightSeqDown.Angles)-1]
					rightReversed := make([]JointAngleSet, len(rightAnglesWithoutLast))
					for i, angle := range rightAnglesWithoutLast {
						rightReversed[len(rightAnglesWithoutLast)-1-i] = angle
					}
					rightSeqDown.Angles = rightReversed
				}

				mergedSequencesDown := []JointSequence{leftSeqDown, rightSeqDown}
				errDown := ws.saveMergedSequence(downName, mergedSequencesDown)

				if errDown != nil {
					response.Success = true
					response.Message = fmt.Sprintf("成功保存UP序列: %s，但DOWN序列保存失败: %v", req.MergedName, errDown)
					response.Data = mergedSequences
				} else {
					response.Success = true
					response.Message = fmt.Sprintf("成功生成序列: %s (UP) 和 %s (DOWN)", req.MergedName, downName)
					response.Data = map[string]interface{}{
						"up":        mergedSequences,
						"down":      mergedSequencesDown,
						"up_file":   req.MergedName + ".json",
						"down_file": downName + ".json",
					}
				}
			}
		} else if isDownMerge {
			// 如果直接合并down序列，按原逻辑处理
			// 反转左臂序列：去掉最后一个，然后反转
			if len(leftSeq.Angles) > 1 {
				leftAnglesWithoutLast := leftSeq.Angles[:len(leftSeq.Angles)-1]
				leftReversed := make([]JointAngleSet, len(leftAnglesWithoutLast))
				for i, angle := range leftAnglesWithoutLast {
					leftReversed[len(leftAnglesWithoutLast)-1-i] = angle
				}
				leftSeq.Angles = leftReversed
			}

			// 反转右臂序列：去掉最后一个，然后反转
			if len(rightSeq.Angles) > 1 {
				rightAnglesWithoutLast := rightSeq.Angles[:len(rightSeq.Angles)-1]
				rightReversed := make([]JointAngleSet, len(rightAnglesWithoutLast))
				for i, angle := range rightAnglesWithoutLast {
					rightReversed[len(rightAnglesWithoutLast)-1-i] = angle
				}
				rightSeq.Angles = rightReversed
			}

			mergedSequences := []JointSequence{leftSeq, rightSeq}
			err := ws.saveMergedSequence(req.MergedName, mergedSequences)
			if err != nil {
				response.Success = false
				response.Message = fmt.Sprintf("保存DOWN序列失败: %v", err)
			} else {
				response.Success = true
				response.Message = fmt.Sprintf("成功合并序列: %s + %s = %s", req.Sequence1Name, req.Sequence2Name, req.MergedName)
				response.Data = mergedSequences
			}
		} else {
			// 既不是up也不是down，按普通合并处理
			mergedSequences := []JointSequence{leftSeq, rightSeq}
			err := ws.saveMergedSequence(req.MergedName, mergedSequences)
			if err != nil {
				response.Success = false
				response.Message = fmt.Sprintf("保存合并序列失败: %v", err)
			} else {
				response.Success = true
				response.Message = fmt.Sprintf("成功合并序列: %s + %s = %s", req.Sequence1Name, req.Sequence2Name, req.MergedName)
				response.Data = mergedSequences
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// saveMergedSequence 保存合并后的序列到根目录
func (ws *WebServer) saveMergedSequence(mergedName string, sequences []JointSequence) error {
	// 生成文件名：直接使用序列名称，替换空格为下划线，确保文件名安全
	fileName := strings.ReplaceAll(mergedName, " ", "_")
	fileName = strings.ReplaceAll(fileName, "/", "_")
	fileName = strings.ReplaceAll(fileName, "\\", "_")
	filePath := fmt.Sprintf("%s.json", fileName) // 保存在根目录

	// 构建完整的JSON配置结构，包含joint_sequences数组
	configData := struct {
		JointSequences []JointSequence `json:"joint_sequences"`
	}{
		JointSequences: sequences,
	}

	// 将配置序列化为JSON
	jsonData, err := json.MarshalIndent(configData, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化序列失败: %v", err)
	}

	// 写入文件
	err = ioutil.WriteFile(filePath, jsonData, 0644)
	if err != nil {
		return fmt.Errorf("写入序列文件失败: %v", err)
	}

	log.Printf("成功保存合并序列: %s 到文件 %s (包含 %d 个序列)", mergedName, filePath, len(sequences))
	return nil
}

// saveJointSequence 保存关节序列到单独的JSON文件
func (ws *WebServer) saveJointSequence(sequence JointSequence) error {
	sequenceDirPath := "json"

	// 确保json目录存在
	if err := ensureJSONDir(sequenceDirPath); err != nil {
		log.Printf("创建json目录失败: %v", err)
	}

	// 生成文件名：直接使用序列名称，替换空格为下划线，确保文件名安全
	fileName := strings.ReplaceAll(sequence.Name, " ", "_")
	fileName = strings.ReplaceAll(fileName, "/", "_")
	fileName = strings.ReplaceAll(fileName, "\\", "_")
	filePath := fmt.Sprintf("%s/%s.json", sequenceDirPath, fileName)

	// 将序列序列化为JSON
	jsonData, err := json.MarshalIndent(sequence, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化序列失败: %v", err)
	}

	// 写入文件
	err = ioutil.WriteFile(filePath, jsonData, 0644)
	if err != nil {
		return fmt.Errorf("写入序列文件失败: %v", err)
	}

	// 更新内存中的配置
	ws.mutex.Lock()
	ws.config.JointSequences = append(ws.config.JointSequences, sequence)
	ws.mutex.Unlock()

	log.Printf("成功保存序列: %s 到文件 %s", sequence.Name, filePath)
	return nil
}

// deleteJointSequence 删除关节序列文件
func (ws *WebServer) deleteJointSequence(sequenceName string) error {
	sequenceDirPath := "json"

	// 生成文件名
	fileName := strings.ReplaceAll(sequenceName, " ", "_")
	fileName = strings.ReplaceAll(fileName, "/", "_")
	fileName = strings.ReplaceAll(fileName, "\\", "_")
	filePath := fmt.Sprintf("%s/%s.json", sequenceDirPath, fileName)

	// 删除文件
	err := os.Remove(filePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除序列文件失败: %v", err)
	}

	// 从内存中移除序列
	ws.mutex.Lock()
	newSequences := make([]JointSequence, 0)
	for _, seq := range ws.config.JointSequences {
		if seq.Name != sequenceName {
			newSequences = append(newSequences, seq)
		}
	}
	ws.config.JointSequences = newSequences
	ws.mutex.Unlock()

	log.Printf("成功删除序列: %s", sequenceName)
	return nil
}

// listMergedSequencesHandler 列出根目录下的合并序列文件
func (ws *WebServer) listMergedSequencesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "只支持GET方法", http.StatusMethodNotAllowed)
		return
	}

	// 读取根目录下的所有JSON文件
	files, err := ioutil.ReadDir(".")
	if err != nil {
		http.Error(w, fmt.Sprintf("读取目录失败: %v", err), http.StatusInternalServerError)
		return
	}

	var mergedFiles []map[string]interface{}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		// 检查是否包含up或down关键字
		fileName := strings.ToLower(file.Name())
		if !strings.Contains(fileName, "up") && !strings.Contains(fileName, "down") {
			continue
		}

		// 尝试读取文件内容，检查是否包含joint_sequences
		data, err := ioutil.ReadFile(file.Name())
		if err != nil {
			continue
		}

		var fileData struct {
			JointSequences []JointSequence `json:"joint_sequences"`
		}
		if err := json.Unmarshal(data, &fileData); err != nil {
			continue
		}

		// 只包含包含左右臂的序列文件
		if len(fileData.JointSequences) >= 2 {
			hasLeft := false
			hasRight := false
			for _, seq := range fileData.JointSequences {
				if seq.ArmType == "left" {
					hasLeft = true
				}
				if seq.ArmType == "right" {
					hasRight = true
				}
			}

			if hasLeft && hasRight {
				mergedFiles = append(mergedFiles, map[string]interface{}{
					"filename": file.Name(),
					"name":     strings.TrimSuffix(file.Name(), ".json"),
					"type": func() string {
						if strings.Contains(fileName, "up") {
							return "up"
						}
						return "down"
					}(),
				})
			}
		}
	}

	response := ControlResponse{
		Success: true,
		Message: "获取合并序列列表成功",
		Data:    mergedFiles,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// executeMergedSequenceHandler 执行合并序列
func (ws *WebServer) executeMergedSequenceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "只支持POST方法", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		FileName string `json:"file_name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "解析请求失败", http.StatusBadRequest)
		return
	}

	// 读取JSON文件
	data, err := ioutil.ReadFile(req.FileName)
	if err != nil {
		http.Error(w, fmt.Sprintf("读取序列文件失败: %v", err), http.StatusNotFound)
		return
	}

	var fileData struct {
		JointSequences []JointSequence `json:"joint_sequences"`
	}
	if err := json.Unmarshal(data, &fileData); err != nil {
		http.Error(w, fmt.Sprintf("解析序列文件失败: %v", err), http.StatusBadRequest)
		return
	}

	// 找到左右臂的控制器
	var leftController, rightController *BlackArmController
	//	leftInterface, rightInterface := "", ""

	ws.mutex.RLock()
	for _, controller := range ws.controllers {
		motorIDs := controller.GetMotorIDs()
		if len(motorIDs) > 0 && motorIDs[0] >= 61 && motorIDs[0] <= 67 {
			leftController = controller
			//		leftInterface = iface
		}
		if len(motorIDs) > 0 && motorIDs[0] >= 51 && motorIDs[0] <= 57 {
			rightController = controller
			//	rightInterface = iface
		}
	}
	ws.mutex.RUnlock()

	if leftController == nil || rightController == nil {
		http.Error(w, "未找到左右臂控制器", http.StatusNotFound)
		return
	}

	// 找到左右臂序列
	var leftSeq, rightSeq *JointSequence
	for i := range fileData.JointSequences {
		if fileData.JointSequences[i].ArmType == "left" {
			leftSeq = &fileData.JointSequences[i]
		}
		if fileData.JointSequences[i].ArmType == "right" {
			rightSeq = &fileData.JointSequences[i]
		}
	}

	if leftSeq == nil || rightSeq == nil {
		http.Error(w, "序列文件中缺少左右臂数据", http.StatusBadRequest)
		return
	}

	// 异步执行左右臂序列
	go ws.executeSequenceAsync(leftController, leftSeq)
	go ws.executeSequenceAsync(rightController, rightSeq)

	response := ControlResponse{
		Success: true,
		Message: fmt.Sprintf("开始执行合并序列: %s", req.FileName),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// executeSequenceFromFile 从文件执行序列（命令行模式）
func executeSequenceFromFile(jsonFile string, config *Config) error {
	// 读取JSON文件
	data, err := ioutil.ReadFile(jsonFile)
	if err != nil {
		return fmt.Errorf("读取序列文件失败: %v", err)
	}

	var fileData struct {
		JointSequences []JointSequence `json:"joint_sequences"`
	}
	if err := json.Unmarshal(data, &fileData); err != nil {
		return fmt.Errorf("解析序列文件失败: %v", err)
	}

	if len(fileData.JointSequences) < 2 {
		return fmt.Errorf("序列文件必须包含左右臂数据")
	}

	// 找到左右臂序列
	var leftSeq, rightSeq *JointSequence
	for i := range fileData.JointSequences {
		if fileData.JointSequences[i].ArmType == "left" {
			leftSeq = &fileData.JointSequences[i]
		}
		if fileData.JointSequences[i].ArmType == "right" {
			rightSeq = &fileData.JointSequences[i]
		}
	}

	if leftSeq == nil || rightSeq == nil {
		return fmt.Errorf("序列文件中缺少左右臂数据")
	}

	// 找到左右臂的接口
	var leftInterface, rightInterface string
	for iface, armConfig := range config.Arms {
		if strings.Contains(armConfig.DeviceName, "left") {
			leftInterface = iface
		}
		if strings.Contains(armConfig.DeviceName, "right") {
			rightInterface = iface
		}
	}

	if leftInterface == "" || rightInterface == "" {
		return fmt.Errorf("未找到左右臂接口配置")
	}

	// 创建左右臂控制器
	leftController := NewBlackArmController(config.CanBridgeURL, leftInterface, "left_black_arm")
	rightController := NewBlackArmController(config.CanBridgeURL, rightInterface, "right_black_arm")

	fileName := strings.ToLower(jsonFile)
	isUp := strings.Contains(fileName, "up")
	isDown := strings.Contains(fileName, "down")
	isSks := strings.Contains(fileName, "sks")

	// 解析手部设备ID
	leftDeviceID := 40
	rightDeviceID := 39
	if strings.HasPrefix(config.Hands["left"].ID, "0x") {
		if id, err := strconv.ParseInt(config.Hands["left"].ID[2:], 16, 32); err == nil {
			leftDeviceID = int(id)
		}
	}
	if strings.HasPrefix(config.Hands["right"].ID, "0x") {
		if id, err := strconv.ParseInt(config.Hands["right"].ID[2:], 16, 32); err == nil {
			rightDeviceID = int(id)
		}
	}

	if isUp {
		// UP序列执行策略
		log.Println("执行UP序列策略")

		// 1. 左右手分别执行防撞预动作
		log.Println("发送左右手防撞预动作")
		sendHandCommandDirect(config.CanBridgeURL, config.Hands["left"].Interface, leftDeviceID, config.HandsLeft)
		sendHandCommandDirect(config.CanBridgeURL, config.Hands["right"].Interface, rightDeviceID, config.HandsRight)
		time.Sleep(500 * time.Millisecond)

		// 2. 清除错误
		log.Println("清除左右臂错误")
		leftController.CleanError()
		rightController.CleanError()
		time.Sleep(200 * time.Millisecond)

		// 3. 使能
		log.Println("使能左右臂")
		leftController.EnableMotor("全部关节")
		rightController.EnableMotor("全部关节")
		time.Sleep(500 * time.Millisecond)

		// 4. 速度设置为0.8
		log.Println("设置左右臂速度为0.8")
		speeds := []float32{0.8, 0.8, 0.8, 0.8, 0.8, 0.8, 0.8}
		leftController.SetSpeeds(speeds)
		rightController.SetSpeeds(speeds)
		time.Sleep(200 * time.Millisecond)

		// 5. 发送关节角度序列（每组之间等待1ms）
		log.Println("执行关节角度序列")
		executeSequenceDirect(leftController, leftSeq, 1*time.Millisecond)
		executeSequenceDirect(rightController, rightSeq, 1*time.Millisecond)

		// 6. 根据json名字发送release_profile
		if isSks {
			log.Println("发送SKS release_profile")
			sendHandCommandDirect(config.CanBridgeURL, config.Hands["left"].Interface, leftDeviceID, config.SksLeftReleaseProfile)
			sendHandCommandDirect(config.CanBridgeURL, config.Hands["right"].Interface, rightDeviceID, config.SksRightReleaseProfile)
		} else {
			log.Println("发送SN release_profile")
			sendHandCommandDirect(config.CanBridgeURL, config.Hands["left"].Interface, leftDeviceID, config.SnLeftReleaseProfile)
			sendHandCommandDirect(config.CanBridgeURL, config.Hands["right"].Interface, rightDeviceID, config.SnRightReleaseProfile)
		}

	} else if isDown {
		// DOWN序列执行策略
		log.Println("执行DOWN序列策略")

		// 1. 手指执行防撞动作
		log.Println("发送左右手防撞预动作")
		sendHandCommandDirect(config.CanBridgeURL, config.Hands["left"].Interface, leftDeviceID, config.HandsLeft)
		sendHandCommandDirect(config.CanBridgeURL, config.Hands["right"].Interface, rightDeviceID, config.HandsRight)
		time.Sleep(500 * time.Millisecond)

		// 2. 速度设为0.8
		log.Println("设置左右臂速度为0.8")
		speeds := []float32{0.8, 0.8, 0.8, 0.8, 0.8, 0.8, 0.8}
		leftController.SetSpeeds(speeds)
		rightController.SetSpeeds(speeds)
		time.Sleep(200 * time.Millisecond)

		// 3. 发送关节角度序列（每组之间等待1ms）
		log.Println("执行关节角度序列")
		executeSequenceDirect(leftController, leftSeq, 1*time.Millisecond)
		executeSequenceDirect(rightController, rightSeq, 1*time.Millisecond)

		// 4. 失能
		log.Println("失能左右臂")
		leftController.DisableMotor()
		rightController.DisableMotor()
		time.Sleep(200 * time.Millisecond)

		// 5. 清除错误
		log.Println("清除左右臂错误")
		leftController.CleanError()
		rightController.CleanError()
	}

	log.Println("序列执行完成")
	return nil
}

// executeSequenceDirect 直接执行序列（不使用goroutine）
func executeSequenceDirect(controller *BlackArmController, sequence *JointSequence, delay time.Duration) {
	for i, angleSet := range sequence.Angles {
		log.Printf("执行第 %d 组角度: %s", i+1, angleSet.Name)

		// 设置每个关节的角度
		for motorIDStr, angle := range angleSet.Values {
			motorID, err := strconv.Atoi(motorIDStr)
			if err != nil {
				log.Printf("无效的电机ID: %s", motorIDStr)
				continue
			}

			err = controller.SetAngle(motorID, angle)
			if err != nil {
				log.Printf("设置电机 %d 角度失败: %v", motorID, err)
			}
		}

		if delay > 0 {
			time.Sleep(delay)
		}
	}
}

// sendHandCommandDirect 直接发送手部命令
func sendHandCommandDirect(canBridgeURL, interfaceName string, deviceID int, values []int) error {
	if len(values) < 6 {
		return fmt.Errorf("手部数据长度不足")
	}

	hand := HandControl{
		Thumb:       values[0],
		ThumbRotate: values[1],
		Index:       values[2],
		Middle:      values[3],
		Ring:        values[4],
		Pinky:       values[5],
	}

	data := []byte{0x01}
	data = append(data, byte(hand.Thumb))
	data = append(data, byte(hand.ThumbRotate))
	data = append(data, byte(hand.Index))
	data = append(data, byte(hand.Middle))
	data = append(data, byte(hand.Ring))
	data = append(data, byte(hand.Pinky))

	canMessage := map[string]interface{}{
		"interface": interfaceName,
		"id":        deviceID,
		"data":      data,
	}

	jsonData, err := json.Marshal(canMessage)
	if err != nil {
		return fmt.Errorf("序列化CAN消息失败: %v", err)
	}

	resp, err := http.Post(canBridgeURL+"/api/can", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("发送CAN消息失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("CAN消息发送失败，状态码: %d", resp.StatusCode)
	}

	return nil
}

func main() {
	// 解析命令行参数
	jsonFile := flag.String("json", "", "要执行的JSON序列文件")
	flag.Parse()

	// 加载配置
	configData, err := ioutil.ReadFile("config.yaml")
	if err != nil {
		log.Fatal("读取配置文件失败:", err)
	}

	var config Config
	if err := yaml.Unmarshal(configData, &config); err != nil {
		log.Fatal("解析配置文件失败:", err)
	}

	// 如果指定了JSON文件，执行序列
	if *jsonFile != "" {
		log.Printf("命令行模式: 执行序列文件 %s", *jsonFile)
		if err := executeSequenceFromFile(*jsonFile, &config); err != nil {
			log.Fatal("执行序列失败:", err)
		}
		return
	}

	// 否则启动Web服务器
	server, err := NewWebServer("config.yaml")
	if err != nil {
		log.Fatal("创建Web服务器失败:", err)
	}

	// 启动服务器
	log.Fatal(server.Start(8080))

}

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// BlackArmController Black Arm控制器结构体
type BlackArmController struct {
	BaseURL   string // CAN桥接服务器URL
	Interface string // CAN接口名称
	MotorIDs  []int  // 电机ID列表
	Client    *http.Client
}

// CANMessage CAN消息结构体
type CANMessage struct {
	Interface string `json:"interface"`
	ID        uint32 `json:"id"`
	Data      []byte `json:"data"`
	Extended  bool   `json:"extended"`
}

// CANResponse CAN响应结构体
type CANResponse struct {
	Status string `json:"status"`
	Data   struct {
		Count int `json:"count"`
	} `json:"data"`
}

// NewBlackArmController 创建新的Black Arm控制器
func NewBlackArmController(baseURL, interface_, deviceName string) *BlackArmController {
	controller := &BlackArmController{
		BaseURL:   baseURL,
		Interface: interface_,
		Client:    &http.Client{Timeout: 50 * time.Second},
	}

	// 根据设备名称确定电机ID范围
	var motorIDs []int
	if strings.Contains(deviceName, "left") {
		// 左臂电机ID范围: 61-67
		motorIDs = []int{61, 62, 63, 64, 65, 66, 67}
	} else if strings.Contains(deviceName, "right") {
		// 右臂电机ID范围: 51-57
		motorIDs = []int{51, 52, 53, 54, 55, 56, 57}
	} else {
		// 默认使用右臂电机ID
		motorIDs = []int{51, 52, 53, 54, 55, 56, 57}
		fmt.Printf("警告: 无法从设备名称 '%s' 判断左右臂，使用默认右臂电机ID\n", deviceName)
	}

	controller.MotorIDs = motorIDs
	fmt.Printf("手臂 %s (%s) 的电机ID列表: %v\n", interface_, deviceName, motorIDs)

	return controller
}

// sendCommand 发送CAN命令
func (b *BlackArmController) sendCommand(command CANMessage) error {
	jsonData, err := json.Marshal(command)
	if err != nil {
		return fmt.Errorf("序列化命令失败: %v", err)
	}

	resp, err := b.Client.Post(b.BaseURL+"/api/can", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("发送HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP请求失败，状态码: %d", resp.StatusCode)
	}

	return nil
}

// EnableMotor 启用电机
func (b *BlackArmController) EnableMotor(motorID string) error {
	if motorID == "全部关节" {
		for _, motor := range b.MotorIDs {
			if err := b.enableSingleMotor(motor); err != nil {
				return err
			}
		}
	} else {
		// 解析单个电机ID (假设格式为十六进制字符串)
		var motor int
		_, err := fmt.Sscanf(motorID, "%x", &motor)
		if err != nil {
			return fmt.Errorf("无效的电机ID: %s", motorID)
		}
		return b.enableSingleMotor(motor)
	}
	return nil
}

// enableSingleMotor 启用单个电机
func (b *BlackArmController) enableSingleMotor(motorID int) error {
	// 设置电机模式为PP (Position Profile)
	setModeCommand := CANMessage{
		Interface: b.Interface,
		ID:        0x1200FD00 + uint32(motorID),
		Data:      []byte{0x05, 0x70, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00}, // PP模式
		Extended:  true,
	}

	// 启用电机命令
	enableCommand := CANMessage{
		Interface: b.Interface,
		ID:        0x0300FD00 + uint32(motorID),
		Data:      []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		Extended:  true,
	}

	if err := b.sendCommand(setModeCommand); err != nil {
		return fmt.Errorf("设置电机模式失败: %v", err)
	}

	if err := b.sendCommand(enableCommand); err != nil {
		return fmt.Errorf("启用电机失败: %v", err)
	}

	fmt.Printf("电机 %d 启用成功\n", motorID)
	return nil
}

// DisableMotor 禁用所有电机
func (b *BlackArmController) DisableMotor() error {
	for _, motorID := range b.MotorIDs {
		command := CANMessage{
			Interface: b.Interface,
			ID:        0x0400FD00 + uint32(motorID),
			Data:      []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			Extended:  true,
		}

		if err := b.sendCommand(command); err != nil {
			return fmt.Errorf("禁用电机 %d 失败: %v", motorID, err)
		}
	}

	fmt.Println("所有电机禁用成功")
	return nil
}

// SetMotorZero 设置所有电机零位
func (b *BlackArmController) SetMotorZero() error {
	for _, motorID := range b.MotorIDs {
		if err := b.setSingleMotorZero(motorID); err != nil {
			return err
		}
	}
	fmt.Println("所有电机零位设置成功")
	return nil
}

// SetMotorZeroByIDs 根据电机ID列表设置零位
func (b *BlackArmController) SetMotorZeroByIDs(motorIDs []int) error {
	if len(motorIDs) == 0 {
		// 如果未指定，设置所有电机
		return b.SetMotorZero()
	}

	for _, motorID := range motorIDs {
		if !b.isValidJoint(motorID) {
			fmt.Printf("警告: 电机ID %d 不在有效范围内，跳过\n", motorID)
			continue
		}
		if err := b.setSingleMotorZero(motorID); err != nil {
			return fmt.Errorf("设置电机 %d 零位失败: %v", motorID, err)
		}
	}
	fmt.Printf("电机 %v 零位设置成功\n", motorIDs)
	return nil
}

// setSingleMotorZero 设置单个电机零位
func (b *BlackArmController) setSingleMotorZero(motorID int) error {
	command := CANMessage{
		Interface: b.Interface,
		ID:        0x0600FD00 + uint32(motorID),
		Data:      []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		Extended:  true,
	}

	if err := b.sendCommand(command); err != nil {
		return fmt.Errorf("设置电机 %d 零位失败: %v", motorID, err)
	}

	return nil
}

// CleanError 清除所有电机错误
func (b *BlackArmController) CleanError() error {
	for _, motorID := range b.MotorIDs {
		command := CANMessage{
			Interface: b.Interface,
			ID:        0x0400FD00 + uint32(motorID),
			Data:      []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			Extended:  true,
		}

		if err := b.sendCommand(command); err != nil {
			return fmt.Errorf("清除电机 %d 错误失败: %v", motorID, err)
		}
	}

	fmt.Println("所有电机错误清除成功")
	return nil
}

// SetAngle 设置单个关节角度
func (b *BlackArmController) SetAngle(jointID int, angle float32) error {
	if !b.isValidJoint(jointID) {
		return fmt.Errorf("无效的关节ID: %d", jointID)
	}
	// 将float32转换为字节数组 (小端序)
	angleBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(angleBytes, math.Float32bits(angle))

	data := []byte{0x16, 0x70, 0x00, 0x00}
	data = append(data, angleBytes...)

	command := CANMessage{
		Interface: b.Interface,
		ID:        0x1200FD00 + uint32(jointID),
		Data:      data,
		Extended:  true,
	}

	if err := b.sendCommand(command); err != nil {
		return fmt.Errorf("设置关节 %d 角度失败: %v", jointID, err)
	}

	fmt.Printf("关节 %d 角度设置为 %.2f\n", jointID, angle)
	return nil
}

// SetAngles 设置所有关节角度
func (b *BlackArmController) SetAngles(angles []float32) error {
	if len(angles) != len(b.MotorIDs) {
		return fmt.Errorf("角度数量 %d 与电机数量 %d 不匹配", len(angles), len(b.MotorIDs))
	}
	fmt.Println("设置所有关节角度", angles)
	// 并发设置所有关节角度
	errChan := make(chan error, len(b.MotorIDs))

	for i, motorID := range b.MotorIDs {
		go func(id int, angle float32) {
			errChan <- b.SetAngle(id, angle)
		}(motorID, angles[i])
	}

	// 等待所有goroutine完成
	for i := 0; i < len(b.MotorIDs); i++ {
		if err := <-errChan; err != nil {
			return err
		}
	}

	fmt.Println("所有关节角度设置成功")
	return nil
}

// 设置所有关节速度
func (b *BlackArmController) SetSpeeds(speeds []float32) error {
	if len(speeds) != len(b.MotorIDs) {
		return fmt.Errorf("速度数量 %d 与电机数量 %d 不匹配", len(speeds), len(b.MotorIDs))
	}

	for i, motorID := range b.MotorIDs {
		err := b.SetSpeed(motorID, speeds[i])
		if err != nil {
			return err
		}
	}
	return nil
}

// SetSpeed 设置单个关节速度
func (b *BlackArmController) SetSpeed(jointID int, speed float32) error {
	if !b.isValidJoint(jointID) {
		return fmt.Errorf("无效的关节ID: %d", jointID)
	}

	// 将float32转换为字节数组 (小端序)
	speedBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(speedBytes, math.Float32bits(speed))

	data := []byte{0x24, 0x70, 0x00, 0x00}
	data = append(data, speedBytes...)

	command := CANMessage{
		Interface: b.Interface,
		ID:        0x1200FD00 + uint32(jointID),
		Data:      data,
		Extended:  true,
	}

	if err := b.sendCommand(command); err != nil {
		return fmt.Errorf("设置关节 %d 速度失败: %v", jointID, err)
	}

	fmt.Printf("关节 %d 速度设置为 %.2f\n", jointID, speed)
	return nil
}

// SetMotorLocKp 设置电机位置Kp参数
func (b *BlackArmController) SetMotorLocKp(motorID int, kp float32) error {
	if !b.isValidJoint(motorID) {
		return fmt.Errorf("无效的电机ID: %d", motorID)
	}

	kpBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(kpBytes, math.Float32bits(kp))

	data := []byte{0x1E, 0x70, 0x00, 0x00}
	data = append(data, kpBytes...)

	command := CANMessage{
		Interface: b.Interface,
		ID:        0x1200FD00 + uint32(motorID),
		Data:      data,
		Extended:  true,
	}

	return b.sendCommand(command)
}

// SetMotorSpeedKp 设置电机速度Kp参数
func (b *BlackArmController) SetMotorSpeedKp(motorID int, kp float32) error {
	if !b.isValidJoint(motorID) {
		return fmt.Errorf("无效的电机ID: %d", motorID)
	}

	kpBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(kpBytes, math.Float32bits(kp))

	data := []byte{0x1F, 0x70, 0x00, 0x00}
	data = append(data, kpBytes...)

	command := CANMessage{
		Interface: b.Interface,
		ID:        0x1200FD00 + uint32(motorID),
		Data:      data,
		Extended:  true,
	}

	return b.sendCommand(command)
}

// SetMotorSpeedKi 设置电机速度Ki参数
func (b *BlackArmController) SetMotorSpeedKi(motorID int, ki float32) error {
	if !b.isValidJoint(motorID) {
		return fmt.Errorf("无效的电机ID: %d", motorID)
	}

	kiBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(kiBytes, math.Float32bits(ki))

	data := []byte{0x20, 0x70, 0x00, 0x00}
	data = append(data, kiBytes...)

	command := CANMessage{
		Interface: b.Interface,
		ID:        0x1200FD00 + uint32(motorID),
		Data:      data,
		Extended:  true,
	}

	return b.sendCommand(command)
}

// SetMotorSpeedFiltGain 设置电机速度滤波增益
func (b *BlackArmController) SetMotorSpeedFiltGain(motorID int, gain float32) error {
	if !b.isValidJoint(motorID) {
		return fmt.Errorf("无效的电机ID: %d", motorID)
	}

	gainBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(gainBytes, math.Float32bits(gain))

	data := []byte{0x21, 0x70, 0x00, 0x00}
	data = append(data, gainBytes...)

	command := CANMessage{
		Interface: b.Interface,
		ID:        0x1200FD00 + uint32(motorID),
		Data:      data,
		Extended:  true,
	}

	return b.sendCommand(command)
}

// ReturnZero 回到零位
func (b *BlackArmController) ReturnZero() error {
	zeroAngles := make([]float32, len(b.MotorIDs))
	// 所有角度设置为0
	for i := range zeroAngles {
		zeroAngles[i] = 0.0
	}

	return b.SetAngles(zeroAngles)
}

// isValidJoint 检查关节ID是否有效
func (b *BlackArmController) isValidJoint(jointID int) bool {
	for _, id := range b.MotorIDs {
		if id == jointID {
			return true
		}
	}
	return false
}

// GetMotorIDs 获取电机ID列表
func (b *BlackArmController) GetMotorIDs() []int {
	return b.MotorIDs
}

// ----------------------------------查询相关----------------------------------
// 查询相关常量
const (
	typeReadSingle = 0x11
	defaultHostID  = 0xFD

	idxLocRef = 0x7016
	idxLocKp  = 0x701E
	idxSpdKp  = 0x701F
	idxSpdKi  = 0x7020
)

// QueryCurrentAngles 查询当前角度值
func QueryCurrentAngles(canBridgeURL, interfaceName string, motorIDs []int) (map[int]float64, map[string]float64, error) {
	client := &http.Client{Timeout: 50 * time.Second}

	// 发送读取请求
	indices := []uint16{idxLocRef, idxLocKp, idxSpdKp, idxSpdKi}
	for _, m := range motorIDs {
		if err := sendReadForMotor(client, canBridgeURL, interfaceName, m, indices); err != nil {
			return nil, nil, fmt.Errorf("发送读取请求失败: %v", err)
		}
	}

	// 监听反馈
	angles := make(map[int]float64)
	params := make(map[string]float64)

	// 先查询第一个电机的参数值（所有电机参数值相同）
	if len(motorIDs) > 0 {
		locKp, spdKp, spdKi := listenMotorParams(client, canBridgeURL, interfaceName, motorIDs[0], 500*time.Millisecond)
		if locKp != nil {
			params["loc_kp"] = *locKp
		}
		if spdKp != nil {
			params["spd_kp"] = *spdKp
		}
		if spdKi != nil {
			params["spd_ki"] = *spdKi
		}
	}

	// 查询所有电机的角度值
	for _, m := range motorIDs {
		ok, angle := listenOneMotorRound(client, canBridgeURL, interfaceName, m, 2000*time.Millisecond)
		if ok {
			angles[m] = angle
		}
	}

	return angles, params, nil
}

// buildReadReqID 构建读取请求ID
func buildReadReqID(hostID, motorID uint8) uint32 {
	return (uint32(typeReadSingle) << 24) | (uint32(hostID) << 8) | uint32(motorID)
}

// buildReadRespID 构建读取响应ID
func buildReadRespID(hostID, motorID uint8) uint32 {
	return (uint32(typeReadSingle) << 24) | (uint32(motorID) << 8) | uint32(hostID)
}

// buildReadReqData 构建读取请求数据
func buildReadReqData(index uint16) []byte {
	d := make([]byte, 8)
	d[0] = byte(index & 0xFF)
	d[1] = byte((index >> 8) & 0xFF)
	return d
}

// sendReadForMotor 发送单个电机的读取请求
func sendReadForMotor(client *http.Client, canBridgeURL, iface string, motorID int, indices []uint16) error {
	for _, idx := range indices {
		id := buildReadReqID(defaultHostID, uint8(motorID))
		data := buildReadReqData(idx)
		cmd := CANMessage{
			Interface: iface,
			ID:        id,
			Data:      data,
			Extended:  true,
		}

		jsonData, err := json.Marshal(cmd)
		if err != nil {
			return fmt.Errorf("序列化命令失败: %v", err)
		}

		url := strings.TrimRight(canBridgeURL, "/") + "/api/can"
		resp, err := client.Post(url, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			return fmt.Errorf("发送HTTP请求失败: %v", err)
		}
		resp.Body.Close()

		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

// listenResponse 监听响应结构
type listenResponse struct {
	Status string `json:"status"`
	Data   struct {
		Messages []struct {
			HexData []string `json:"hex_data"`
		} `json:"messages"`
	} `json:"data"`
}

// parseHexByte 解析十六进制字节
func parseHexByte(s string) byte {
	v, _ := strconv.ParseUint(s, 16, 8)
	return byte(v)
}

// listenOneMotorRound 监听单个电机一轮（仅loc_ref）
func listenOneMotorRound(client *http.Client, canBridgeURL, iface string, motorID int, maxDuration time.Duration) (bool, float64) {
	if maxDuration <= 0 {
		maxDuration = 2000 * time.Millisecond
	}
	respID := buildReadRespID(defaultHostID, uint8(motorID))
	listenBase := strings.TrimRight(canBridgeURL, "/") + "/api/messages"
	url := fmt.Sprintf("%s/%s?id=%d", listenBase, iface, respID)

	type streamState struct {
		seen     map[uint32]bool
		prevBits uint32
		prevVal  float32
		havePrev bool
		repeats  int
		captured bool
	}
	state := streamState{seen: make(map[uint32]bool)}

	deadline := time.Now().Add(maxDuration)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err != nil {
			time.Sleep(60 * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var lr listenResponse
		if err := json.Unmarshal(body, &lr); err != nil || len(lr.Data.Messages) == 0 {
			time.Sleep(80 * time.Millisecond)
			continue
		}

		for _, m := range lr.Data.Messages {
			if len(m.HexData) < 8 {
				continue
			}
			idx := uint16(parseHexByte(m.HexData[0])) |
				uint16(parseHexByte(m.HexData[1]))<<8

			b4 := parseHexByte(m.HexData[4])
			b5 := parseHexByte(m.HexData[5])
			b6 := parseHexByte(m.HexData[6])
			b7 := parseHexByte(m.HexData[7])
			u := uint32(b4) | uint32(b5)<<8 | uint32(b6)<<16 | uint32(b7)<<24
			val32 := math.Float32frombits(u)

			// 仅对 loc_ref 应用重复判定逻辑
			if idx == idxLocRef {
				if state.seen[u] {
					state.repeats++
					if !state.captured && state.havePrev {
						return true, float64(state.prevVal)
					}
					if state.repeats >= 2 {
						return true, float64(state.prevVal)
					}
				} else {
					state.seen[u] = true
					state.prevBits = u
					state.prevVal = val32
					state.havePrev = true
				}
			}
		}
		time.Sleep(80 * time.Millisecond)
	}
	return false, 0
}

// listenMotorParams 监听电机参数值（loc_kp, spd_kp, spd_ki）
func listenMotorParams(client *http.Client, canBridgeURL, iface string, motorID int, maxDuration time.Duration) (*float64, *float64, *float64) {
	respID := buildReadRespID(defaultHostID, uint8(motorID))
	listenBase := strings.TrimRight(canBridgeURL, "/") + "/api/messages"
	url := fmt.Sprintf("%s/%s?id=%d", listenBase, iface, respID)

	var locKp, spdKp, spdKi *float64
	seen := make(map[uint16]bool)

	deadline := time.Now().Add(maxDuration)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err != nil {
			time.Sleep(60 * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var lr listenResponse
		if err := json.Unmarshal(body, &lr); err != nil || len(lr.Data.Messages) == 0 {
			time.Sleep(80 * time.Millisecond)
			continue
		}

		for _, m := range lr.Data.Messages {
			if len(m.HexData) < 8 {
				continue
			}
			idx := uint16(parseHexByte(m.HexData[0])) |
				uint16(parseHexByte(m.HexData[1]))<<8

			b4 := parseHexByte(m.HexData[4])
			b5 := parseHexByte(m.HexData[5])
			b6 := parseHexByte(m.HexData[6])
			b7 := parseHexByte(m.HexData[7])
			u := uint32(b4) | uint32(b5)<<8 | uint32(b6)<<16 | uint32(b7)<<24
			val32 := math.Float32frombits(u)

			if idx == idxLocKp && !seen[idxLocKp] {
				v := float64(val32)
				locKp = &v
				seen[idxLocKp] = true
			}
			if idx == idxSpdKp && !seen[idxSpdKp] {
				v := float64(val32)
				spdKp = &v
				seen[idxSpdKp] = true
			}
			if idx == idxSpdKi && !seen[idxSpdKi] {
				v := float64(val32)
				spdKi = &v
				seen[idxSpdKi] = true
			}

			if locKp != nil && spdKp != nil && spdKi != nil {
				return locKp, spdKp, spdKi
			}
		}
		time.Sleep(80 * time.Millisecond)
	}
	return locKp, spdKp, spdKi
}

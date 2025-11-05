let devices = {
    arms: [],
    hands: []
};
let isUpdating = false;
let currentInterface = ''; // 用于保存序列对话框
let tempRecordCounter = 1; // 临时记录计数器

// 页面加载时初始化
document.addEventListener('DOMContentLoaded', function() {
    loadAllDevices();
});

// 加载所有设备
async function loadAllDevices() {
    try {
        showLoading(true);
        // 并行加载机械臂和手部设备
        const [armsResponse, handsResponse] = await Promise.all([
            fetch('/api/arms'),
            fetch('/api/hands')
        ]);

        const arms = await armsResponse.json();
        const hands = await handsResponse.json();

        devices.arms = arms || [];
        devices.hands = hands || [];

        console.log('加载的设备:', { arms, hands });

        createDevicePanels();
        updateStatus();
        loadAllSequencesForMerge(); // 加载所有序列到合并区域
        loadMergedSequences(); // 加载合并后的序列列表
        showLoading(false);
    } catch (error) {
        console.error('加载设备失败:', error);
        showNotification('加载设备失败: ' + error.message, 'error');
        showLoading(false);
    }
}

// 创建设备面板
function createDevicePanels() {
    const container = document.getElementById('deviceGrid');
    container.innerHTML = '';
    
    // 固定渲染顺序：左臂 -> 右臂 -> 左手 -> 右手
    const renderOrder = [
{ type: 'arm', side: 'left' },
{ type: 'arm', side: 'right' },
{ type: 'hand', side: 'left' },
{ type: 'hand', side: 'right' }
    ];
    
    renderOrder.forEach(item => {
        let device = null;
        let panel = null;

        if (item.type === 'arm') {
            // 查找对应的臂设备
            device = devices.arms.find(arm => arm.arm_type === item.side);
            if (device) {
                panel = createArmPanel(device);
            }
        } else if (item.type === 'hand') {
            // 查找对应的手设备
            device = devices.hands.find(hand => hand.hand_type === item.side);
            if (device) {
                panel = createHandPanel(device);
            }
        }

        // 如果找到设备并创建了面板，则添加到容器中
        if (panel) {
            container.appendChild(panel);
        }
    });
    
    document.getElementById('deviceGrid').style.display = 'grid';
}

// 创建机械臂面板
function createArmPanel(arm) {
    const panel = document.createElement('div');
    panel.className = 'device-panel';
    panel.id = `arm-${arm.interface}`;
    
    const armTypeLabel = arm.arm_type === 'left' ? '(左臂)' : arm.arm_type === 'right' ? '(右臂)' : '';
    
    panel.innerHTML = `
<div class="device-header">
    <div class="device-title">🤖 ${arm.interface} - ${arm.device_name} ${armTypeLabel}</div>
    <button class="btn btn-success control-btn" onclick="queryCurrentAngles('${arm.interface}')">查询</button>
</div>

<div class="joint-controls">
    <div class="flex-row-gap-10 margin-bottom-8">
        <h4 class="margin-0">🎯 关节控制 (${arm.motor_ids.length} 个电机)</h4>
        <div class="batch-angle-wrapper">
            <input type="text" id="batchAngles-${arm.interface}" class="modal-input" placeholder="批量设置角度: 61: 0.0, 62: 0.0, ...">
            <button class="btn btn-primary control-btn" onclick="setBatchAngles('${arm.interface}')">应用</button>
        </div>
    </div>
    <div class="joint-sliders-container">
        <div class="joint-sliders" id="jointSliders-${arm.interface}"></div>
    </div>
</div>

<div class="system-controls">
    <div class="system-controls-header">
        <h4>⚙️ 系统控制</h4>
        <div class="params-display">
            <span>参数：</span>
            <span>loc_kp: <span id="displayLocKp-${arm.interface}" class="param-value-display">-</span></span>
            <span>spd_kp: <span id="displaySpdKp-${arm.interface}" class="param-value-display">-</span></span>
            <span>spd_ki: <span id="displaySpdKi-${arm.interface}" class="param-value-display">-</span></span>
        </div>
    </div>
    <div class="system-controls-row">
        <div class="system-controls-buttons">
            <button class="btn btn-success control-btn" onclick="enableArm('${arm.interface}')">启用</button>
            <button class="btn btn-danger control-btn" onclick="disableArm('${arm.interface}')">禁用</button>
            <button class="btn btn-warning control-btn" onclick="setZero('${arm.interface}')">设零</button>
            <button class="btn btn-primary control-btn" onclick="returnZero('${arm.interface}')">回零</button>
            <button class="btn btn-warning control-btn" onclick="cleanError('${arm.interface}')">清错</button>
        </div>
        <div class="system-controls-speed">
            <label>全局速度：</label>
            <input type="number" id="globalSpeed-${arm.interface}" class="speed-input" min="0" max="10" step="0.1" value="1.0" placeholder="全局速度">
            <button class="btn btn-success control-btn" onclick="setAllSpeeds('${arm.interface}')">应用</button>
            <button class="btn btn-success control-btn" onclick="setAllSpeeds03('${arm.interface}', 0.3)">0.3</button>
        </div>
    </div>
</div>

<div class="angle-sequence-section">
    <h5>📐 角度序列管理</h5>
    <div class="angle-sequence-layout">
        <div class="temp-records-container" id="tempRecords-${arm.interface}">
            <div class="sequence-buttons-row">
                <button class="btn btn-primary control-btn" onclick="recordCurrentAngles('${arm.interface}')">记录</button>
                <button class="btn btn-danger control-btn" onclick="clearTempRecords('${arm.interface}')">清除</button>
                <button class="btn btn-success control-btn" onclick="showSaveSequenceDialog('${arm.interface}')">保存</button>
            </div>
            <div class="temp-record-list" id="tempRecordList-${arm.interface}"></div>
        </div>
        
        <div class="saved-sequences-container">
            <div class="sequence-buttons-row with-title">
                <h6>已保存序列</h6>
                <button class="btn btn-success control-btn" onclick="executeSelectedSequences('${arm.interface}')">执行</button>
                <button class="btn btn-danger control-btn" onclick="deleteSelectedSequences('${arm.interface}')">删除</button>
                <button class="btn btn-primary control-btn margin-left-auto" onclick="refreshSequences('${arm.interface}')">刷新</button>
            </div>
            <div class="sequence-list" id="sequenceList-${arm.interface}"></div>
        </div>
    </div>
</div>

<div class="pid-controls">
    <h5 onclick="togglePIDControls('${arm.interface}')" class="cursor-pointer">🔧 PID参数调节 <span id="pidToggle-${arm.interface}">▼</span></h5>
    <div id="pidControlsContent-${arm.interface}" class="display-none">
        ${createPIDControlHTML(arm.interface, 'locKp', '位置Kp', 0, 1000, 100, 1)}
        ${createPIDControlHTML(arm.interface, 'speedKp', '速度Kp', 0, 500, 50, 1)}
        ${createPIDControlHTML(arm.interface, 'speedKi', '速度Ki', 0, 100, 10, 1)}
        ${createPIDControlHTML(arm.interface, 'filtGain', '滤波增益', 0, 1, 0.1, 0.01)}
    </div>
</div>
    `;
    
    // 创建关节滑块
    setTimeout(() => {
        createJointSliders(arm);
        setupPIDControls(arm.interface);
        refreshTempRecords(arm.interface);
        refreshSequences(arm.interface);
    }, 100);
    
    return panel;
}

// 创建 PID 控制 HTML（辅助函数）
function createPIDControlHTML(interfaceName, paramName, label, min, max, defaultValue, step) {
    return `
<div class="param-control">
    <div class="param-label">
        <span>${label}</span>
        <span class="param-value" id="${paramName}Value-${interfaceName}">${defaultValue}</span>
    </div>
    <div class="joint-controls-row">
        <div class="slider-container">
            <input type="range" class="slider" id="${paramName}Slider-${interfaceName}" 
                   min="${min}" max="${max}" value="${defaultValue}" step="${step}">
        </div>
        <div class="angle-input-container">
            <input type="number" class="angle-input" id="${paramName}Input-${interfaceName}" 
                   min="${min}" max="${max}" step="${step}" value="${defaultValue}">
            <div class="angle-btn-container">
                <button class="angle-btn" onclick="adjustParam('${interfaceName}', '${paramName}', ${step})">▲</button>
                <button class="angle-btn" onclick="adjustParam('${interfaceName}', '${paramName}', -${step})">▼</button>
            </div>
        </div>
    </div>
</div>
    `;
}


// 创建手部面板
function createHandPanel(hand) {
    const panel = document.createElement('div');
    panel.className = 'device-panel';
    panel.id = `hand-${hand.interface}`;
    
    const handTypeLabel = hand.hand_type === 'left' ? '(左手)' : hand.hand_type === 'right' ? '(右手)' : '';
    
    panel.innerHTML = `
<div class="device-header">
    <div class="device-title">✋ ${hand.interface} - ${hand.device_name} ${handTypeLabel} (ID: ${hand.device_id})</div>
</div>

<div class="hand-controls">
    <div class="flex-row-gap-10 margin-bottom-8">
        <h4 class="margin-0">🎵 手指控制</h4>
        <div class="hand-type-selector margin-left-auto">
            <label for="handTypeSelector-${hand.interface}" class="font-weight-bold color-2c3e50 font-size-085em" style="margin-right: 5px;">乐器类型:</label>
            <select id="handTypeSelector-${hand.interface}" class="hand-type-select">
                <option value="sks">萨克斯 (SKS)</option>
                <option value="sn" selected>唢呐 (SN)</option>
            </select>
        </div>
    </div>
    <div class="finger-sliders-container">
        <div class="finger-sliders" id="fingerSliders-${hand.interface}"></div>
    </div>
    
    <div class="preset-buttons">
        <button class="btn btn-primary preset-btn" onclick="setHandPreset('${hand.interface}', 'press')">按压</button>
        <button class="btn btn-success preset-btn" onclick="setHandPreset('${hand.interface}', 'release')">松开</button>
        <button class="btn btn-warning preset-btn display-none" onclick="setHandPreset('${hand.interface}', 'high_thumb')" id="highThumbBtn-${hand.interface}">高音拇指</button>
        <button class="btn btn-danger preset-btn display-none" onclick="setHandPreset('${hand.interface}', 'high_pro_thumb')" id="highProThumbBtn-${hand.interface}">倍高音拇指</button>
        <button class="btn btn-primary preset-btn" onclick="testHandControl('${hand.interface}')">测试</button>
        <button class="btn btn-success preset-btn" onclick="resetAllFingers('${hand.interface}')">重置</button>
    </div>

    <div class="save-config-section">
        <h4>💾 保存到外部配置文件</h4>
        <div class="save-buttons">
            <button class="btn btn-primary preset-btn" onclick="saveCurrentToConfig('${hand.interface}', 'press')">保存按压位置</button>
            <button class="btn btn-success preset-btn" onclick="saveCurrentToConfig('${hand.interface}', 'release')">保存松开位置</button>
            <button class="btn btn-warning preset-btn display-none" onclick="saveCurrentToConfig('${hand.interface}', 'high_thumb')" id="saveHighThumbBtn-${hand.interface}">保存高音拇指</button>
            <button class="btn btn-danger preset-btn display-none" onclick="saveCurrentToConfig('${hand.interface}', 'high_pro_thumb')" id="saveHighProThumbBtn-${hand.interface}">保存倍高音拇指</button>
        </div>
    </div>
</div>
    `;
    
    // 创建手指滑块
    setTimeout(() => {
        createFingerSliders(hand);
        setupHandTypeSelector(hand.interface);
        // 初始化时更新预设按钮显示（默认使用sn）
        updateHandPresetButtons(hand.interface);
    }, 100);
    
    return panel;
}

// 创建关节滑块
function createJointSliders(arm) {
    const container = document.getElementById(`jointSliders-${arm.interface}`);
    if (!container) return;
    
    container.innerHTML = '';

    arm.motor_ids.forEach((motorID, index) => {
        const sliderDiv = document.createElement('div');
        sliderDiv.className = 'joint-slider';

        sliderDiv.innerHTML = `
    <!-- 角度控制 -->
    <div class="joint-control-panel" id="anglePanel${index}-${arm.interface}">
        <div class="joint-controls-row-compact">
            <span class="joint-label-compact">关节${index + 1} (ID:${motorID})</span>
            <div class="slider-container">
                <input type="range" class="slider" id="joint${index}Slider-${arm.interface}" 
                       min="-3.14" max="3.14" value="0" step="0.01">
            </div>
            <div class="angle-input-container">
                <input type="number" class="angle-input" id="joint${index}Input-${arm.interface}" 
                       min="-3.14" max="3.14" step="0.01" value="0.00"
                       title="当前值: 0.00">
            </div>
            <div class="angle-btn-container">
                <button class="angle-btn" onclick="adjustJointValue('${arm.interface}', ${index}, 0.01)">▲</button>
                <button class="angle-btn" onclick="adjustJointValue('${arm.interface}', ${index}, -0.01)">▼</button>
            </div>
            <div class="joint-tabs">
                <button class="tab-btn active" onclick="switchTab('${arm.interface}', ${index}, 'angle')">角度</button>
                <button class="tab-btn" onclick="switchTab('${arm.interface}', ${index}, 'speed')">速度</button>
            </div>
        </div>
    </div>
    
    <!-- 速度控制 -->
    <div class="joint-control-panel display-none" id="speedPanel${index}-${arm.interface}">
        <div class="joint-controls-row-compact">
            <span class="joint-label-compact">关节${index + 1} (ID:${motorID})</span>
            <div class="slider-container">
                <input type="range" class="slider" id="speed${index}Slider-${arm.interface}" 
                       min="0" max="10" value="1" step="0.1">
            </div>
            <div class="angle-input-container">
                <input type="number" class="angle-input" id="speed${index}Input-${arm.interface}" 
                       min="0" max="10" step="0.1" value="1.0"
                       title="当前值: 1.00">
            </div>
            <div class="joint-tabs">
                <button class="tab-btn" onclick="switchTab('${arm.interface}', ${index}, 'angle')">角度</button>
                <button class="tab-btn active" onclick="switchTab('${arm.interface}', ${index}, 'speed')">速度</button>
            </div>
        </div>
    </div>
        `;
        
        container.appendChild(sliderDiv);

        // 设置滑块事件
        setupJointSlider(arm.interface, index, motorID);
    });
}

// 设置关节滑块事件
function setupJointSlider(interfaceName, jointIndex, motorID) {
    // 设置角度控制事件
    const angleSlider = document.getElementById(`joint${jointIndex}Slider-${interfaceName}`);
    const angleValueInput = document.getElementById(`joint${jointIndex}Input-${interfaceName}`);
    
    if (angleSlider && angleValueInput) {
// 角度滑块事件
angleSlider.addEventListener('input', function() {
    const value = parseFloat(this.value);
    angleValueInput.value = value.toFixed(2);
    angleValueInput.title = `当前值: ${value.toFixed(2)}`;
    
    if (!isUpdating) {
setJointAngle(interfaceName, motorID, value);
    }
});

// 角度输入框事件 - blur时更新滑动条，由滑动条触发事件
angleValueInput.addEventListener('input', function() {
    // 输入时不响应，只显示
});

angleValueInput.addEventListener('blur', function() {
    const value = parseFloat(this.value);
    if (isNaN(value) || value < -3.14 || value > 3.14) {
// 恢复为滑块值
this.value = parseFloat(angleSlider.value).toFixed(2);
this.title = `当前值: ${parseFloat(angleSlider.value).toFixed(2)}`;
    } else {
// blur时更新滑动条的值，让滑动条触发input事件发送命令
angleSlider.value = value;
this.value = value.toFixed(2);
this.title = `当前值: ${value.toFixed(2)}`;
// 触发滑动条的input事件，由滑动条发送命令
if (!isUpdating) {
    angleSlider.dispatchEvent(new Event('input'));
}
    }
});
    }
    
    // 设置速度控制事件
    const speedSlider = document.getElementById(`speed${jointIndex}Slider-${interfaceName}`);
    const speedValueInput = document.getElementById(`speed${jointIndex}Input-${interfaceName}`);
    
    if (speedSlider && speedValueInput) {
// 初始化速度显示
speedValueInput.value = '1.0';
speedValueInput.title = '当前值: 1.00';

// 速度滑块事件
speedSlider.addEventListener('input', function() {
    const value = parseFloat(this.value);
    speedValueInput.value = value.toFixed(1);
    speedValueInput.title = `当前值: ${value.toFixed(2)}`;
    
if (!isUpdating) {
setJointSpeed(interfaceName, motorID, value);
    }
});

// 速度输入框事件 - blur时更新滑动条，由滑动条触发事件
speedValueInput.addEventListener('input', function() {
    // 输入时不响应，只显示
});

speedValueInput.addEventListener('blur', function() {
    const value = parseFloat(this.value);
    if (isNaN(value) || value < 0 || value > 10) {
// 恢复为滑块值
this.value = parseFloat(speedSlider.value).toFixed(1);
this.title = `当前值: ${parseFloat(speedSlider.value).toFixed(2)}`;
    } else {
// blur时更新滑动条的值，让滑动条触发input事件发送命令
speedSlider.value = value;
this.value = value.toFixed(1);
this.title = `当前值: ${value.toFixed(2)}`;
// 触发滑动条的input事件，由滑动条发送命令
if (!isUpdating) {
    speedSlider.dispatchEvent(new Event('input'));
}
    }
});
    }
}

// 创建手指滑块
function createFingerSliders(hand) {
    const container = document.getElementById(`fingerSliders-${hand.interface}`);
    if (!container) return;
    
    container.innerHTML = '';

    const fingerNames = ['拇指', '拇指旋转', '食指', '中指', '无名指', '小指'];
    const fingerKeys = ['thumb', 'thumbRotate', 'index', 'middle', 'ring', 'pinky'];

    fingerKeys.forEach((key, index) => {
        const sliderDiv = document.createElement('div');
        sliderDiv.className = 'finger-slider';

        sliderDiv.innerHTML = `
    <div class="joint-controls-row-compact">
        <span class="finger-label-compact">${fingerNames[index]}</span>
        <div class="slider-container">
            <input type="range" class="slider" id="${key}Slider-${hand.interface}" 
                   min="0" max="255" value="255" step="1">
        </div>
        <div class="angle-input-container">
            <input type="number" class="finger-input" id="${key}Input-${hand.interface}" 
                   min="0" max="255" step="1" value="255">
        </div>
        <div class="angle-btn-container">
            <button class="angle-btn" onclick="adjustFingerValue('${hand.interface}', '${key}', 1)">▲</button>
            <button class="angle-btn" onclick="adjustFingerValue('${hand.interface}', '${key}', -1)">▼</button>
        </div>
    </div>
        `;

        container.appendChild(sliderDiv);

        // 设置滑块事件
        setupFingerSlider(hand.interface, key);
    });
}

// 设置手指滑块事件
function setupFingerSlider(interfaceName, fingerKey) {
    const slider = document.getElementById(`${fingerKey}Slider-${interfaceName}`);
    const valueInput = document.getElementById(`${fingerKey}Input-${interfaceName}`);
    
    if (!slider || !valueInput) return;
    
    // 滑块事件 - 立即响应后端
slider.addEventListener('input', function() {
    const value = parseInt(this.value);
    valueInput.value = value;
    
    if (!isUpdating) {
setFingerPosition(interfaceName, fingerKey, value);
    }
});

    // 输入框事件 - blur时更新滑动条，由滑动条触发事件
valueInput.addEventListener('input', function() {
    // 输入时不响应，只显示
});

valueInput.addEventListener('blur', function() {
    const value = parseInt(this.value);
    if (isNaN(value) || value < 0 || value > 255) {
// 恢复为滑块值
this.value = slider.value;
    } else {
// blur时更新滑动条的值，让滑动条触发input事件发送命令
slider.value = value;
this.value = value;
// 触发滑动条的input事件，由滑动条发送命令
if (!isUpdating) {
    slider.dispatchEvent(new Event('input'));
}
    }
});
}

// 设置PID控制事件
function setupPIDControls(interfaceName) {
    const params = ['locKp', 'speedKp', 'speedKi', 'filtGain'];
    
    params.forEach(param => {
const slider = document.getElementById(`${param}Slider-${interfaceName}`);
const valueDisplay = document.getElementById(`${param}Value-${interfaceName}`);
const valueInput = document.getElementById(`${param}Input-${interfaceName}`);

if (!slider || !valueDisplay || !valueInput) return;

const step = param === 'filtGain' ? 0.01 : 1;

slider.addEventListener('input', function() {
    const value = parseFloat(this.value);
    valueDisplay.textContent = value.toFixed(step < 1 ? 2 : 1);
    valueInput.value = value.toFixed(step < 1 ? 2 : 1);
    
    if (!isUpdating) {
updatePIDParameter(interfaceName, param, value);
    }
});

valueInput.addEventListener('input', function() {
    const value = parseFloat(this.value);
    if (!isNaN(value)) {
slider.value = value;
valueDisplay.textContent = value.toFixed(step < 1 ? 2 : 1);

if (!isUpdating) {
    updatePIDParameter(interfaceName, param, value);
}
    }
});
    });
}

// 设置手部类型选择器（镜像联动）
function setupHandTypeSelector(interfaceName) {
    const selector = document.getElementById(`handTypeSelector-${interfaceName}`);
    if (!selector) return;
    
    selector.addEventListener('change', function() {
const selectedValue = this.value;

// 同步更新所有手部的选择器（镜像联动）
devices.hands.forEach(hand => {
    const otherSelector = document.getElementById(`handTypeSelector-${hand.interface}`);
    if (otherSelector && otherSelector !== this) {
otherSelector.value = selectedValue;
updateHandPresetButtons(hand.interface);
    }
});

// 更新当前手部的预设按钮
updateHandPresetButtons(interfaceName);
    });
}

// 更新手部预设按钮显示
function updateHandPresetButtons(interfaceName) {
    const localSelector = document.getElementById(`handTypeSelector-${interfaceName}`);
    const handType = localSelector ? localSelector.value : 'sn';
    const highThumbBtn = document.getElementById(`highThumbBtn-${interfaceName}`);
    const highProThumbBtn = document.getElementById(`highProThumbBtn-${interfaceName}`);
    const saveHighThumbBtn = document.getElementById(`saveHighThumbBtn-${interfaceName}`);
    const saveHighProThumbBtn = document.getElementById(`saveHighProThumbBtn-${interfaceName}`);
    
    // 获取当前手部设备信息
    const currentHand = devices.hands.find(hand => hand.interface === interfaceName);
    const isLeftHand = currentHand && currentHand.hand_type === 'left';
    
    // 只有左手且为唢呐类型才显示高音和倍高音按钮
    if (handType === 'sn' && isLeftHand) {
        if (highThumbBtn) highThumbBtn.classList.remove('display-none');
        if (highProThumbBtn) highProThumbBtn.classList.remove('display-none');
        if (saveHighThumbBtn) saveHighThumbBtn.classList.remove('display-none');
        if (saveHighProThumbBtn) saveHighProThumbBtn.classList.remove('display-none');
    } else {
        if (highThumbBtn) highThumbBtn.classList.add('display-none');
        if (highProThumbBtn) highProThumbBtn.classList.add('display-none');
        if (saveHighThumbBtn) saveHighThumbBtn.classList.add('display-none');
        if (saveHighProThumbBtn) saveHighProThumbBtn.classList.add('display-none');
    }
}

// 切换标签页
function switchTab(interfaceName, jointIndex, tabType) {
    // 更新标签按钮状态
    const angleBtn = document.querySelector(`#arm-${interfaceName} .joint-slider:nth-child(${jointIndex + 1}) .tab-btn:nth-child(1)`);
    const speedBtn = document.querySelector(`#arm-${interfaceName} .joint-slider:nth-child(${jointIndex + 1}) .tab-btn:nth-child(2)`);
    
    if (angleBtn && speedBtn) {
        angleBtn.classList.toggle('active', tabType === 'angle');
        speedBtn.classList.toggle('active', tabType === 'speed');
    }
    
    // 切换面板显示
    const anglePanel = document.getElementById(`anglePanel${jointIndex}-${interfaceName}`);
    const speedPanel = document.getElementById(`speedPanel${jointIndex}-${interfaceName}`);
    
    if (anglePanel && speedPanel) {
        if (tabType === 'angle') {
            anglePanel.classList.remove('display-none');
            speedPanel.classList.add('display-none');
        } else {
            anglePanel.classList.add('display-none');
            speedPanel.classList.remove('display-none');
        }
    }
}

// 微调关节值
function adjustJointValue(interfaceName, jointIndex, delta) {
    const slider = document.getElementById(`joint${jointIndex}Slider-${interfaceName}`);
    const valueInput = document.getElementById(`joint${jointIndex}Input-${interfaceName}`);
    
    if (!slider || !valueInput) return;
    
    const currentValue = parseFloat(slider.value);
    const newValue = Math.max(-3.14, Math.min(3.14, currentValue + delta));
    
    slider.value = newValue;
    valueInput.value = newValue.toFixed(2);
    valueInput.title = `当前值: ${newValue.toFixed(2)}`;
    
    // 获取对应的电机ID
    const arm = devices.arms.find(a => a.interface === interfaceName);
    if (arm && arm.motor_ids[jointIndex]) {
setJointAngle(interfaceName, arm.motor_ids[jointIndex], newValue);
    }
}

// 微调速度值
function adjustSpeedValue(interfaceName, jointIndex, delta) {
    const slider = document.getElementById(`speed${jointIndex}Slider-${interfaceName}`);
    const valueInput = document.getElementById(`speed${jointIndex}Input-${interfaceName}`);
    
    if (!slider || !valueInput) return;
    
    const currentValue = parseFloat(slider.value);
    const newValue = Math.max(0, Math.min(10, currentValue + delta));
    
    slider.value = newValue;
    valueInput.value = newValue.toFixed(1);
    valueInput.title = `当前值: ${newValue.toFixed(2)}`;
    
    // 获取对应的电机ID
    const arm = devices.arms.find(a => a.interface === interfaceName);
    if (arm && arm.motor_ids[jointIndex]) {
setJointSpeed(interfaceName, arm.motor_ids[jointIndex], newValue);
    }
}

// 微调手指值
function adjustFingerValue(interfaceName, fingerKey, delta) {
    const slider = document.getElementById(`${fingerKey}Slider-${interfaceName}`);
    const valueInput = document.getElementById(`${fingerKey}Input-${interfaceName}`);
    
    if (!slider || !valueInput) return;
    
    const currentValue = parseInt(slider.value);
    const newValue = Math.max(0, Math.min(255, currentValue + delta));
    
    slider.value = newValue;
    valueInput.value = newValue;
    
    // 触发滑动条的input事件，由滑动条发送命令
    if (!isUpdating) {
slider.dispatchEvent(new Event('input'));
    }
}

// 微调参数
function adjustParam(interfaceName, paramType, delta) {
    const slider = document.getElementById(`${paramType}Slider-${interfaceName}`);
    const valueDisplay = document.getElementById(`${paramType}Value-${interfaceName}`);
    const valueInput = document.getElementById(`${paramType}Input-${interfaceName}`);
    
    if (!slider || !valueDisplay || !valueInput) return;
    
    const currentValue = parseFloat(slider.value);
    const newValue = Math.max(parseFloat(slider.min), Math.min(parseFloat(slider.max), currentValue + delta));
    
    slider.value = newValue;
    const step = paramType === 'filtGain' ? 0.01 : 1;
    valueDisplay.textContent = newValue.toFixed(step < 1 ? 2 : 1);
    valueInput.value = newValue.toFixed(step < 1 ? 2 : 1);
    
    updatePIDParameter(interfaceName, paramType, newValue);
}

// 批量设置角度
async function setBatchAngles(interfaceName) {
    try {
        const input = document.getElementById(`batchAngles-${interfaceName}`);
        const inputValue = input.value.trim();
        
        if (!inputValue) {
            showNotification('请输入角度值', 'warning');
            return;
        }
        
        // 解析新格式: 61: 0.000000, 62: 0.000000, ...
        const jointMap = {};
        const parts = inputValue.split(',').map(s => s.trim()).filter(s => s);
        
        for (const part of parts) {
            const match = part.match(/^\s*(\d+)\s*:\s*([-\d.]+)\s*$/);
            if (match) {
                const jointID = parseInt(match[1]);
                const angle = parseFloat(match[2]);
                if (!isNaN(jointID) && !isNaN(angle)) {
                    jointMap[jointID] = angle;
                }
            }
        }
        
        if (Object.keys(jointMap).length === 0) {
            showNotification('无法解析角度格式，请使用格式: 61: 0.000000, 62: 0.000000, ...', 'error');
            return;
        }
        
        // 获取当前臂的电机ID列表
        const arm = devices.arms.find(a => a.interface === interfaceName);
        if (!arm) {
            showNotification('未找到机械臂设备', 'error');
            return;
        }
        
        // 验证所有电机ID是否都在范围内
        const missingIDs = arm.motor_ids.filter(id => !(id in jointMap));
        if (missingIDs.length > 0) {
            showNotification(`缺少电机ID: ${missingIDs.join(', ')}`, 'error');
            return;
        }
        
        // 检查角度范围
        for (const [jointID, angle] of Object.entries(jointMap)) {
            if (angle < -3.14 || angle > 3.14) {
                showNotification(`电机 ${jointID} 的角度 ${angle} 超出范围 [-3.14, 3.14]`, 'error');
                return;
            }
        }
        
        // 调用新的批量设置函数
        await setAllJointAngle(interfaceName, jointMap);
        
    } catch (error) {
        console.error('批量设置角度失败:', error);
        showNotification('批量设置角度失败', 'error');
    }
}

// 设置所有关节角度（调用后端set_all_angles方法）
async function setAllJointAngle(interfaceName, jointMap) {
    try {
        // 获取当前臂的电机ID列表
        const arm = devices.arms.find(a => a.interface === interfaceName);
        if (!arm) {
            showNotification('未找到机械臂设备', 'error');
            return;
        }
        
        // 构建joints数组，按照motor_ids的顺序
        const joints = arm.motor_ids.map(jointID => ({
            joint_id: jointID,
            angle: jointMap[jointID] || 0
        }));
        
        // 设置更新标志，避免触发设置命令
        isUpdating = true;
        
        // 发送到后端
        const response = await fetch('/api/joints/', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                interface: interfaceName,
                action: 'set_all_angles',
                joints: joints
            })
        });
        
        const result = await response.json();
        
        if (result.success) {
            // 成功后更新前端滑动条
            arm.motor_ids.forEach((motorID, index) => {
                const slider = document.getElementById(`joint${index}Slider-${interfaceName}`);
                const valueInput = document.getElementById(`joint${index}Input-${interfaceName}`);
                
                if (slider && valueInput) {
                    const angle = jointMap[motorID];
                    slider.value = angle;
                    valueInput.value = angle.toFixed(2);
                    valueInput.title = `当前值: ${angle.toFixed(2)}`;
                }
            });
            
            showNotification(`成功设置所有关节角度`, 'success');
        } else {
            showNotification(`设置所有角度失败: ${result.message}`, 'error');
        }
        
        isUpdating = false;
    } catch (error) {
        console.error('设置所有关节角度失败:', error);
        showNotification('设置所有关节角度失败', 'error');
        isUpdating = false;
    }
}

// 设置关节角度
async function setJointAngle(interfaceName, jointID, angle) {
    try {
const response = await fetch('/api/joints/', {
    method: 'POST',
    headers: {
'Content-Type': 'application/json',
    },
    body: JSON.stringify({
interface: interfaceName,
action: 'set_angle',
joint_id: jointID,
value: angle
    })
});

const result = await response.json();
if (!result.success) {
    console.error(`设置角度失败: ${result.message}`);
    return false;
}
return true;
    } catch (error) {
console.error('设置角度失败:', error);
return false;
    }
}

// 设置关节速度
async function setJointSpeed(interfaceName, jointID, speed) {
    try {
        const response = await fetch('/api/joints/', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                interface: interfaceName,
                action: 'set_speed',
                joint_id: jointID,
                value: speed
            })
        });
        
        const result = await response.json();
        if (!result.success) {
            console.error(`设置速度失败: ${result.message}`);
            return false;
        }
        console.log(`关节 ${jointID} 速度设置为 ${speed}`);
        return true;
    } catch (error) {
        console.error('设置速度失败:', error);
        return false;
    }
}

// 设置手指位置
async function setFingerPosition(interfaceName, fingerKey, value) {
    try {
const handData = {
    thumb: 0,
    thumb_rotate: 0,
    index: 0,
    middle: 0,
    ring: 0,
    pinky: 0
};

// 键名映射：前端使用 thumbRotate，后端期望 thumb_rotate
const mapFingerKey = (key) => {
    return key === 'thumbRotate' ? 'thumb_rotate' : key;
};

// 更新当前手指的值
const mappedFingerKey = mapFingerKey(fingerKey);
handData[mappedFingerKey] = value;

// 从滑块获取其他手指的当前值
const fingerKeys = ['thumb', 'thumbRotate', 'index', 'middle', 'ring', 'pinky'];
fingerKeys.forEach(key => {
    if (key !== fingerKey) {
const slider = document.getElementById(`${key}Slider-${interfaceName}`);
if (slider) {
    const mappedKey = mapFingerKey(key);
    handData[mappedKey] = parseInt(slider.value);
}
    }
});

console.log(`发送手部控制命令: ${fingerKey}=${value}, 完整数据:`, handData);

const response = await fetch('/api/hand/', {
    method: 'POST',
    headers: {
'Content-Type': 'application/json',
    },
    body: JSON.stringify({
interface: interfaceName,
action: 'set_fingers',
hand: handData
    })
});

const result = await response.json();
if (!result.success) {
    console.error(`设置手指失败: ${result.message}`);
}
    } catch (error) {
console.error('设置手指失败:', error);
    }
}

// 更新PID参数
async function updatePIDParameter(interfaceName, paramType, value) {
    try {
const actionMap = {
    'locKp': 'set_loc_kp',
    'speedKp': 'set_speed_kp',
    'speedKi': 'set_speed_ki',
    'filtGain': 'set_filt_gain'
};

// 获取对应机械臂的第一个电机ID
const arm = devices.arms.find(a => a.interface === interfaceName);
if (!arm || !arm.motor_ids || arm.motor_ids.length === 0) {
    console.error('没有找到机械臂设备或电机ID');
    return;
}

const response = await fetch('/api/joints/', {
    method: 'POST',
    headers: {
'Content-Type': 'application/json',
    },
    body: JSON.stringify({
interface: interfaceName,
action: actionMap[paramType],
joint_id: arm.motor_ids[0],
value: value
    })
});

const result = await response.json();
if (!result.success) {
    console.error(`更新${paramType}失败: ${result.message}`);
}
    } catch (error) {
console.error('更新参数失败:', error);
    }
}

// 设置手部预设位置
async function setHandPreset(interfaceName, profile) {
    try {
const localSelector = document.getElementById(`handTypeSelector-${interfaceName}`);
const handType = localSelector ? localSelector.value : 'sn';

const response = await fetch('/api/hand/', {
    method: 'POST',
    headers: {
'Content-Type': 'application/json',
    },
    body: JSON.stringify({
interface: interfaceName,
action: 'set_profile',
hand_type: handType,
profile: profile
    })
});

const result = await response.json();
if (result.success) {
    showNotification(`${interfaceName} 设置${profile}预设成功`, 'success');
    
    // 如果返回了预设值，更新前端滑动条
    if (result.data && result.data.profile_values) {
updateFingerSlidersFromProfile(interfaceName, result.data.profile_values);
    }
} else {
    showNotification(`${interfaceName} 设置${profile}预设失败: ${result.message}`, 'error');
}
    } catch (error) {
console.error('设置预设失败:', error);
showNotification('设置预设失败', 'error');
    }
}

// 测试手部控制功能
async function testHandControl(interfaceName) {
    try {
showNotification(`开始测试${interfaceName}手部控制...`, 'warning');

const testHandData = {
    thumb: 230,
    thumb_rotate: 230,
    index: 230,
    middle: 230,
    ring: 230,
    pinky: 230
};

const response = await fetch('/api/hand/', {
    method: 'POST',
    headers: {
'Content-Type': 'application/json',
    },
    body: JSON.stringify({
interface: interfaceName,
action: 'set_fingers',
hand: testHandData
    })
});

const result = await response.json();
if (result.success) {
    showNotification(`${interfaceName}手部控制测试成功！`, 'success');
    // 更新滑块显示
    updateFingerSliders(interfaceName, testHandData);
} else {
    showNotification(`${interfaceName}手部控制测试失败: ${result.message}`, 'error');
}
    } catch (error) {
console.error('手部控制测试失败:', error);
showNotification('手部控制测试失败', 'error');
    }
}

// 重置所有手指
async function resetAllFingers(interfaceName) {
    try {
const resetHandData = {
    thumb: 255,
    thumb_rotate: 255,
    index: 255,
    middle: 255,
    ring: 255,
    pinky: 255
};

const response = await fetch('/api/hand/', {
    method: 'POST',
    headers: {
'Content-Type': 'application/json',
    },
    body: JSON.stringify({
interface: interfaceName,
action: 'set_fingers',
hand: resetHandData
    })
});

const result = await response.json();
if (result.success) {
    showNotification(`${interfaceName}所有手指已重置`, 'success');
    // 更新滑块显示
    updateFingerSliders(interfaceName, resetHandData);
    } else {
    showNotification(`${interfaceName}重置失败: ${result.message}`, 'error');
}
    } catch (error) {
console.error('重置失败:', error);
showNotification('重置失败', 'error');
    }
}

// 根据预设值更新手指滑动条
function updateFingerSlidersFromProfile(interfaceName, profileValues) {
    if (!profileValues || profileValues.length !== 6) {
        console.error('预设值格式不正确:', profileValues);
        return;
    }
    
    const fingerKeys = ['thumb', 'thumbRotate', 'index', 'middle', 'ring', 'pinky'];
    
    // 设置更新标志，避免触发设置命令
    isUpdating = true;
    
    fingerKeys.forEach((key, index) => {
        const slider = document.getElementById(`${key}Slider-${interfaceName}`);
        const valueInput = document.getElementById(`${key}Input-${interfaceName}`);
        
        if (slider && valueInput) {
            const value = profileValues[index];
            slider.value = value;
            valueInput.value = value;
            valueInput.title = `当前值: ${value}`;
        }
    });
    
    isUpdating = false;
}

// 更新手指滑块显示
function updateFingerSliders(interfaceName, handData) {
    const fingerKeys = ['thumb', 'thumbRotate', 'index', 'middle', 'ring', 'pinky'];
    
    // 设置更新标志，避免触发设置命令
    isUpdating = true;
    
    fingerKeys.forEach(key => {
        const slider = document.getElementById(`${key}Slider-${interfaceName}`);
        const valueInput = document.getElementById(`${key}Input-${interfaceName}`);
        
        if (slider && valueInput) {
            const value = handData[key === 'thumbRotate' ? 'thumb_rotate' : key] || 0;
            slider.value = value;
            valueInput.value = value;
            valueInput.title = `当前值: ${value}`;
        }
    });
    
    isUpdating = false;
}

// 保存当前手指位置到外部配置文件
async function saveCurrentToConfig(interfaceName, profile) {
    try {
const localSelector = document.getElementById(`handTypeSelector-${interfaceName}`);
const handType = localSelector ? localSelector.value : 'sn';

// 获取当前所有手指的值
const currentValues = [];
const fingerKeys = ['thumb', 'thumbRotate', 'index', 'middle', 'ring', 'pinky'];
fingerKeys.forEach(key => {
    const slider = document.getElementById(`${key}Slider-${interfaceName}`);
    if (slider) {
currentValues.push(parseInt(slider.value));
    }
});

if (currentValues.length !== 6) {
    showNotification('获取手指位置数据失败', 'error');
    return;
}

// 确定左右手
const hand = devices.hands.find(h => h.interface === interfaceName);
if (!hand) {
    showNotification('未找到手部设备信息', 'error');
    return;
}
const handSide = hand.device_id === 40 ? 'left' : 'right';

console.log(`保存配置: ${handType}_${handSide}_${profile}`, currentValues);

const response = await fetch('/api/config/update', {
    method: 'POST',
    headers: {
'Content-Type': 'application/json',
    },
    body: JSON.stringify({
hand_type: handType,
profile: profile,
values: currentValues,
hand: handSide
    })
});

const result = await response.json();
if (result.success) {
    showNotification(`成功保存 ${handType}_${handSide}_${profile} 到外部配置文件`, 'success');
} else {
    showNotification(`保存失败: ${result.message}`, 'error');
}
    } catch (error) {
console.error('保存配置失败:', error);
showNotification('保存配置失败: ' + error.message, 'error');
    }
}

// 启用机械臂
async function enableArm(interfaceName) {
    await controlArm(interfaceName, 'enable', '启用');
}

// 禁用机械臂
async function disableArm(interfaceName) {
    await controlArm(interfaceName, 'disable', '禁用');
}

// 设置零点 - 显示确认对话框
let currentSetZeroInterface = '';
function setZero(interfaceName) {
    currentSetZeroInterface = interfaceName;
    document.getElementById('setZeroMotorIDs').value = '';
    document.getElementById('setZeroModal').style.display = 'block';
}

// 关闭设置零点对话框
function closeSetZeroDialog() {
    document.getElementById('setZeroModal').style.display = 'none';
}

// 确认设置零点
async function confirmSetZero() {
    const motorIDsInput = document.getElementById('setZeroMotorIDs').value.trim();
    let motorIDs = [];
    
    // 解析电机ID输入
    if (motorIDsInput) {
const ids = motorIDsInput.split(',').map(id => id.trim()).filter(id => id);
motorIDs = ids.map(id => parseInt(id)).filter(id => !isNaN(id));

if (motorIDs.length === 0) {
    showNotification('无效的电机ID格式', 'error');
    return;
}
    }
    
    // 发送设置零点请求
    try {
const response = await fetch('/api/arm/', {
    method: 'POST',
    headers: {
'Content-Type': 'application/json',
    },
    body: JSON.stringify({
interface: currentSetZeroInterface,
action: 'set_zero',
motor_ids: motorIDs
    })
});

const result = await response.json();
if (result.success) {
    showNotification(result.message, 'success');
    closeSetZeroDialog();
} else {
    showNotification(result.message, 'error');
}
    } catch (error) {
console.error('设置零点失败:', error);
showNotification('设置零点失败', 'error');
    }
}

// 回零
async function returnZero(interfaceName) {
    const result = await controlArm(interfaceName, 'return_zero', '回零');
    
    // 回零成功后，更新所有滑动条为0
    if (result && result.success) {
const arm = devices.arms.find(a => a.interface === interfaceName);
if (arm) {
    isUpdating = true;
    arm.motor_ids.forEach((motorID, index) => {
const angleSlider = document.getElementById(`joint${index}Slider-${interfaceName}`);
const angleValueDisplay = document.getElementById(`joint${index}Value-${interfaceName}`);
const angleValueInput = document.getElementById(`joint${index}Input-${interfaceName}`);

if (angleSlider && angleValueInput) {
    angleSlider.value = 0;
    angleValueInput.value = '0.00';
    angleValueInput.title = '当前值: 0.00';
}
    });
    isUpdating = false;
}
    }
}
// 清除错误
async function cleanError(interfaceName) {
    await controlArm(interfaceName, 'clean_error', '清除错误');
}

// 控制机械臂
async function controlArm(interfaceName, action, actionName) {
    try {
const response = await fetch('/api/arm/', {
    method: 'POST',
    headers: {
'Content-Type': 'application/json',
    },
    body: JSON.stringify({
interface: interfaceName,
action: action
    })
});

const result = await response.json();
if (result.success) {
    showNotification(`${interfaceName} ${actionName}成功`, 'success');
} else {
    showNotification(`${interfaceName} ${actionName}失败: ${result.message}`, 'error');
}
return result;
    } catch (error) {
console.error(`${actionName}失败:`, error);
showNotification(`${interfaceName} ${actionName}失败`, 'error');
return { success: false };
    }
}

// 设置所有关节速度
async function setAllSpeeds(interfaceName) {
    try {
        const globalSpeedInput = document.getElementById(`globalSpeed-${interfaceName}`);
        const speed = parseFloat(globalSpeedInput.value);
        
        if (isNaN(speed) || speed < 0 || speed > 10) {
            showNotification('请输入有效的速度值 (0-10)', 'warning');
            return;
        }
        
        const arm = devices.arms.find(a => a.interface === interfaceName);
        if (!arm) {
            showNotification('未找到机械臂设备', 'error');
            return;
        }
        
        // 更新所有速度滑块和输入框
        arm.motor_ids.forEach((motorID, index) => {
            const speedSlider = document.getElementById(`speed${index}Slider-${interfaceName}`);
            const speedValueDisplay = document.getElementById(`speed${index}Value-${interfaceName}`);
            const speedValueInput = document.getElementById(`speed${index}Input-${interfaceName}`);
            
            if (speedSlider && speedValueDisplay && speedValueInput) {
                speedSlider.value = speed;
                speedValueDisplay.textContent = speed.toFixed(2);
                speedValueInput.value = speed.toFixed(1);
            }
        });
        
        // 发送所有速度设置命令，等待所有完成
        const promises = arm.motor_ids.map((motorID, index) => {
            return setJointSpeed(interfaceName, motorID, speed);
        });
        
        const results = await Promise.all(promises);
        const allSuccess = results.every(result => result === true);
        
        if (allSuccess) {
            showNotification(`已设置所有关节速度为 ${speed}`, 'success');
        } else {
            showNotification('部分关节速度设置失败', 'error');
        }
    } catch (error) {
        console.error('设置所有速度失败:', error);
        showNotification('设置所有速度失败', 'error');
    }
}
//设置所有关节速度为0.3
async function setAllSpeeds03(interfaceName) {
    try {
        const arm = devices.arms.find(a => a.interface === interfaceName);
        if (!arm) {
            showNotification('未找到机械臂设备', 'error');
            return;
        }
        const speed = 0.3;
        
        // 更新所有速度滑块和输入框
        arm.motor_ids.forEach((motorID, index) => {
            const speedSlider = document.getElementById(`speed${index}Slider-${interfaceName}`);
            const speedValueDisplay = document.getElementById(`speed${index}Value-${interfaceName}`);
            const speedValueInput = document.getElementById(`speed${index}Input-${interfaceName}`);
            
            if (speedSlider && speedValueDisplay && speedValueInput) {
                speedSlider.value = speed;
                speedValueDisplay.textContent = speed.toFixed(2);
                speedValueInput.value = speed.toFixed(1);
            }
        });
        
        // 发送所有速度设置命令，等待所有完成
        const promises = arm.motor_ids.map((motorID, index) => {
            return setJointSpeed(interfaceName, motorID, speed);
        });
        
        const results = await Promise.all(promises);
        const allSuccess = results.every(result => result === true);
        
        if (allSuccess) {
            showNotification(`已设置所有关节速度为 0.3`, 'success');
        } else {
            showNotification('部分关节速度设置失败', 'error');
        }
    } catch (error) {
        console.error('设置所有速度为0.3失败:', error);
        showNotification('设置所有速度为0.3失败', 'error');
    }
}

// 显示加载状态
function showLoading(show) {
    const loading = document.getElementById('loading');
    if (loading) {
loading.style.display = show ? 'block' : 'none';
    }
}

// 更新状态显示
function updateStatus() {
    const totalDevices = devices.arms.length + devices.hands.length;
    const currentInterface = document.getElementById('currentInterface');
    const motorCount = document.getElementById('motorCount');
    const lastUpdate = document.getElementById('lastUpdate');
    const connectionStatus = document.getElementById('connectionStatus');
    
    if (currentInterface) currentInterface.textContent = `${totalDevices} 个设备`;
    if (motorCount) motorCount.textContent = devices.arms.reduce((sum, arm) => sum + (arm.motor_ids ? arm.motor_ids.length : 0), 0);
    if (lastUpdate) lastUpdate.textContent = new Date().toLocaleTimeString();
    
    if (connectionStatus) {
connectionStatus.className = totalDevices > 0 ? 'status-indicator' : 'status-indicator error';
    }
}

// 显示通知
function showNotification(message, type = 'info') {
    const notification = document.getElementById('notification');
    if (!notification) return;
    
    notification.textContent = message;
    notification.className = `notification ${type}`;
    notification.classList.add('show');
    
    setTimeout(() => {
notification.classList.remove('show');
    }, 3000);
}

// ========== 角度序列管理功能 ==========

// 记录当前角度
async function recordCurrentAngles(interfaceName) {
    try {
const name = `角度组 ${tempRecordCounter}`;
tempRecordCounter++;

// 获取当前角度值（从滑块读取）
const currentAngles = {};
const arm = devices.arms.find(a => a.interface === interfaceName);
if (!arm) {
    showNotification('未找到机械臂设备', 'error');
    return;
}

arm.motor_ids.forEach((motorID, index) => {
    const slider = document.getElementById(`joint${index}Slider-${interfaceName}`);
if (slider) {
currentAngles[motorID.toString()] = parseFloat(slider.value);
    }
});

const response = await fetch('/api/joint-sequences/temp/', {
    method: 'POST',
    headers: {
'Content-Type': 'application/json',
    },
    body: JSON.stringify({
interface: interfaceName,
name: name,
angles: currentAngles
    })
});

const result = await response.json();
if (result.success) {
    showNotification(`已记录角度组: ${name}`, 'success');
    refreshTempRecords(interfaceName);
} else {
    showNotification(`记录失败: ${result.message}`, 'error');
}
    } catch (error) {
console.error('记录角度失败:', error);
showNotification('记录角度失败', 'error');
    }
}

// 清除临时记录
async function clearTempRecords(interfaceName) {
    try {
const response = await fetch(`/api/joint-sequences/temp/?interface=${interfaceName}`, {
    method: 'DELETE'
});

const result = await response.json();
if (result.success) {
    showNotification('已清除临时记录', 'success');
    refreshTempRecords(interfaceName);
    tempRecordCounter = 1; // 重置计数器
} else {
    showNotification(`清除失败: ${result.message}`, 'error');
}
    } catch (error) {
console.error('清除临时记录失败:', error);
showNotification('清除临时记录失败', 'error');
    }
}

// 刷新临时记录显示
async function refreshTempRecords(interfaceName) {
    try {
const response = await fetch(`/api/joint-sequences/temp/?interface=${interfaceName}`);
const result = await response.json();

const container = document.getElementById(`tempRecordList-${interfaceName}`);
if (!container) return;

container.innerHTML = '';

if (result.success && result.data && result.data.length > 0) {
    result.data.forEach(record => {
const item = document.createElement('div');
item.className = 'temp-record-item';
item.textContent = record.name;
container.appendChild(item);
    });
    } else {
    container.innerHTML = '<div style="color: #999; font-size: 0.8em; text-align: center; padding: 10px;">暂无记录</div>';
}
    } catch (error) {
console.error('刷新临时记录失败:', error);
    }
}

// 显示保存序列对话框
function showSaveSequenceDialog(interfaceName) {
    currentInterface = interfaceName;
    document.getElementById('sequenceName').value = '';
    document.getElementById('saveSequenceModal').style.display = 'block';
}

// 关闭保存序列对话框
function closeSaveSequenceDialog() {
    document.getElementById('saveSequenceModal').style.display = 'none';
}

// 保存序列
async function saveSequence() {
    try {
        const name = document.getElementById('sequenceName').value.trim();
        const armModel = document.getElementById('saveArmModelSelector').value;

        if (!name) {
            showNotification('请输入序列名称', 'warning');
            return;
        }

        const response = await fetch('/api/joint-sequences/', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                interface: currentInterface,
                name: name,
                arm_model: armModel
            })
        });

const result = await response.json();
if (result.success) {
    showNotification('序列保存成功', 'success');
    closeSaveSequenceDialog();
    refreshTempRecords(currentInterface);
    refreshSequences(currentInterface);
} else {
    showNotification(`保存失败: ${result.message}`, 'error');
}
    } catch (error) {
console.error('保存序列失败:', error);
showNotification('保存序列失败', 'error');
    }
}

// 刷新序列列表
async function refreshSequences(interfaceName) {
    try {
// 同时刷新全局合并区域的序列列表
loadAllSequencesForMerge();

const response = await fetch('/api/joint-sequences/');
const result = await response.json();

const container = document.getElementById(`sequenceList-${interfaceName}`);
if (!container) return;

container.innerHTML = '';

if (result.success && result.data && result.data.length > 0) {
    // 获取当前接口的臂类型
    const currentArm = devices.arms.find(arm => arm.interface === interfaceName);
    const currentArmType = currentArm ? currentArm.arm_type : null;
    
    // 过滤序列：优先匹配arm_type，其次匹配interface
    const filteredSequences = result.data.filter(seq => {
// 如果序列有arm_type字段，优先按arm_type匹配
if (seq.arm_type && currentArmType) {
    return seq.arm_type === currentArmType;
}
// 向后兼容：如果没有arm_type，按interface匹配
return seq.interface === interfaceName;
    });
    
    if (filteredSequences.length > 0) {
// 创建序列网格容器（每行两个）
let row = null;
filteredSequences.forEach((sequence, idx) => {
    // 每两个元素创建一行
    if (idx % 2 === 0) {
        row = document.createElement('div');
        row.style.display = 'flex';
        row.style.gap = '4px';
        row.style.marginBottom = '2px';
        container.appendChild(row);
    }
    
    const item = document.createElement('div');
    item.style.display = 'flex';
    item.style.alignItems = 'center';
    item.style.gap = '4px';
    item.style.flex = '1';
    item.style.padding = '2px 4px';
    item.style.background = '#f8f9fa';
    item.style.borderRadius = '4px';
    
    item.innerHTML = `
        <input type="checkbox" class="sequence-checkbox" value="${sequence.name}" style="cursor: pointer;">
        <span class="sequence-name" style="font-size: 0.85em; flex: 1;">${sequence.name}</span>
    `;
    
    row.appendChild(item);
    });
} else {
container.innerHTML = '<div style="color: #999; font-size: 0.8em; text-align: center; padding: 10px;">暂无序列</div>';
    }
} else {
    container.innerHTML = '<div style="color: #999; font-size: 0.8em; text-align: center; padding: 10px;">暂无序列</div>';
}
    } catch (error) {
console.error('刷新序列列表失败:', error);
    }
}

// 执行选中的序列
async function executeSelectedSequences(interfaceName) {
    const checkboxes = document.querySelectorAll(`#sequenceList-${interfaceName} .sequence-checkbox:checked`);
    if (checkboxes.length === 0) {
showNotification('请先选择要执行的序列', 'warning');
return;
    }
    
    const sequenceNames = Array.from(checkboxes).map(cb => cb.value);
    const confirmMsg = `确定要执行 ${sequenceNames.length} 个序列吗？\n${sequenceNames.join(', ')}`;
    
    if (!confirm(confirmMsg)) {
return;
    }
    
    // 执行选中的序列
    for (const sequenceName of sequenceNames) {
await executeSequence(interfaceName, sequenceName);
// 序列之间稍微延迟
await new Promise(resolve => setTimeout(resolve, 500));
    }
}

// 执行序列
async function executeSequence(interfaceName, sequenceName) {
    try {
const response = await fetch('/api/joint-sequences/execute/', {
    method: 'POST',
    headers: {
'Content-Type': 'application/json',
    },
    body: JSON.stringify({
interface: interfaceName,
sequence_name: sequenceName
    })
});

const result = await response.json();
if (result.success) {
    showNotification(`开始执行序列: ${sequenceName}`, 'success');
    // 开始监控角度更新
    startAngleMonitoring(interfaceName);
} else {
    showNotification(`执行失败: ${result.message}`, 'error');
}
    } catch (error) {
console.error('执行序列失败:', error);
showNotification('执行序列失败', 'error');
    }
}

// 删除选中的序列
async function deleteSelectedSequences(interfaceName) {
    const checkboxes = document.querySelectorAll(`#sequenceList-${interfaceName} .sequence-checkbox:checked`);
    if (checkboxes.length === 0) {
showNotification('请先选择要删除的序列', 'warning');
return;
    }
    
    const sequenceNames = Array.from(checkboxes).map(cb => cb.value);
    const confirmMsg = `确定要删除 ${sequenceNames.length} 个序列吗？\n${sequenceNames.join(', ')}\n\n此操作不可恢复！`;
    
    if (!confirm(confirmMsg)) {
return;
    }
    
    // 删除选中的序列
    let successCount = 0;
    let failCount = 0;
    
    for (const sequenceName of sequenceNames) {
try {
    const response = await fetch('/api/joint-sequences/', {
method: 'DELETE',
    headers: {
'Content-Type': 'application/json',
    },
    body: JSON.stringify({
    sequence_name: sequenceName
    })
});

const result = await response.json();
if (result.success) {
successCount++;
} else {
failCount++;
console.error(`删除序列 ${sequenceName} 失败:`, result.message);
}
    } catch (error) {
    failCount++;
    console.error(`删除序列 ${sequenceName} 失败:`, error);
}
    }
    
    if (successCount > 0) {
showNotification(`成功删除 ${successCount} 个序列`, 'success');
refreshSequences(interfaceName);
    }
    if (failCount > 0) {
showNotification(`${failCount} 个序列删除失败`, 'error');
    }
}

// 开始监控角度更新
function startAngleMonitoring(interfaceName) {
    let monitoringCount = 0;
    const maxMonitoringTime = 30; // 最多监控30秒
    
    const monitor = setInterval(async () => {
try {
    await updateAnglesFromServer(interfaceName);
    monitoringCount++;
    
    // 30秒后停止监控
    if (monitoringCount >= maxMonitoringTime) {
clearInterval(monitor);
    }
} catch (error) {
    console.error('更新角度失败:', error);
    clearInterval(monitor);
}
    }, 1000); // 每秒更新一次
}

// 从服务器获取当前角度并更新滑动条
async function updateAnglesFromServer(interfaceName) {
    try {
const response = await fetch(`/api/current-angles/?interface=${interfaceName}`);
const result = await response.json();

if (result.success && result.data) {
    const angles = result.data;
    const arm = devices.arms.find(a => a.interface === interfaceName);
    
    if (arm) {
// 设置更新标志，避免触发设置命令
isUpdating = true;

// 更新每个关节的滑动条显示
arm.motor_ids.forEach((motorID, index) => {
    const motorIDStr = motorID.toString();
    if (angles[motorIDStr] !== undefined) {
        const angle = angles[motorIDStr];
        
        // 更新角度滑动条
        const slider = document.getElementById(`joint${index}Slider-${interfaceName}`);
        const valueInput = document.getElementById(`joint${index}Input-${interfaceName}`);
        
        if (slider && valueInput) {
            slider.value = angle;
            valueInput.value = angle.toFixed(2);
            valueInput.title = `当前值: ${angle.toFixed(2)}`;
        }
    }
});

isUpdating = false;
    }
}
    } catch (error) {
console.error('获取当前角度失败:', error);
isUpdating = false;
    }
}

// 切换PID控制面板显示/隐藏
function togglePIDControls(interfaceName) {
    const content = document.getElementById(`pidControlsContent-${interfaceName}`);
    const toggle = document.getElementById(`pidToggle-${interfaceName}`);
    
    if (content.style.display === 'none') {
content.style.display = 'block';
toggle.textContent = '▲';
} else {
content.style.display = 'none';
toggle.textContent = '▼';
    }
}

// 加载所有序列到全局合并区域
async function loadAllSequencesForMerge() {
    try {
const response = await fetch('/api/joint-sequences/');
const result = await response.json();

const container = document.getElementById('allSequencesList');
const mergeSection = document.getElementById('globalMergeSection');

if (!container || !mergeSection) return;

container.innerHTML = '';

if (result.success && result.data && result.data.length > 0) {
    result.data.forEach(sequence => {
const item = document.createElement('div');
item.className = 'global-sequence-item';

const armTypeDisplay = sequence.arm_type === 'left' ? '左臂' : sequence.arm_type === 'right' ? '右臂' : '';
const armTypeColor = sequence.arm_type === 'left' ? '#667eea' : '#e74c3c';

item.innerHTML = `
    <input type="checkbox" class="global-sequence-checkbox" value="${sequence.name}" 
           data-arm-type="${sequence.arm_type}"
           onchange="updateGlobalMergeButton()">
    <span class="global-sequence-type" style="color: ${armTypeColor};">${armTypeDisplay}</span>
    <span class="global-sequence-name" style="font-weight: bold;">${sequence.name}</span>
    <span class="global-sequence-count">${sequence.angles ? sequence.angles.length : 0}组</span>
`;

// 点击整个卡片也能切换复选框
item.addEventListener('click', function(e) {
    if (e.target.type !== 'checkbox') {
        const checkbox = this.querySelector('.global-sequence-checkbox');
        if (checkbox) {
            checkbox.checked = !checkbox.checked;
            updateGlobalMergeButton();
        }
    }
});

container.appendChild(item);
    });
    
    const containerDiv = document.getElementById('mergeExecuteContainer');
    if (containerDiv) {
        containerDiv.style.display = 'grid';
    }
} else {
    const containerDiv = document.getElementById('mergeExecuteContainer');
    if (containerDiv) {
        // 只有在两个区域都没有数据时才隐藏
        const mergedContainer = document.getElementById('mergedSequencesList');
        if (!mergedContainer || mergedContainer.children.length === 0) {
            containerDiv.style.display = 'none';
        }
    }
}
    } catch (error) {
console.error('加载全局序列列表失败:', error);
    }
}

// 更新全局合并按钮状态
function updateGlobalMergeButton() {
    const checkboxes = document.querySelectorAll('.global-sequence-checkbox:checked');
    const mergeBtn = document.getElementById('globalMergeBtn');
    
    if (mergeBtn) {
// 检查是否选中了恰好2个序列,并且一个是left一个是right
if (checkboxes.length === 2) {
    const armTypes = Array.from(checkboxes).map(cb => cb.dataset.armType);
    const hasLeft = armTypes.includes('left');
    const hasRight = armTypes.includes('right');
    mergeBtn.disabled = !(hasLeft && hasRight);
} else {
    mergeBtn.disabled = true;
}
    }
}


// 关闭合并对话框
function closeMergeDialog() {
    document.getElementById('mergeSequenceModal').style.display = 'none';
}

// 执行合并
async function mergeSequences() {
    try {
        const mergedName = document.getElementById('mergedSequenceName').value.trim();
        const armModel = document.getElementById('mergeArmModelSelector').value;

        if (!mergedName) {
            showNotification('请输入合并后的序列名称', 'warning');
            return;
        }

        if (!window.selectedSequenceNames || window.selectedSequenceNames.length !== 2) {
            showNotification('请选择两个序列', 'warning');
            return;
        }

        const response = await fetch('/api/joint-sequences/merge/', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                sequence1_name: window.selectedSequenceNames[0],
                sequence2_name: window.selectedSequenceNames[1],
                merged_name: mergedName,
                arm_model: armModel
            })
        });

const result = await response.json();
if (result.success) {
    showNotification(`合并成功！文件已保存为 ${mergedName}.json`, 'success');
    closeMergeDialog();
    
    // 清除全局选中状态
    const checkboxes = document.querySelectorAll('.global-sequence-checkbox');
    checkboxes.forEach(cb => cb.checked = false);
    updateGlobalMergeButton();
    
    // 刷新合并序列列表
    await loadMergedSequences();
} else {
    showNotification(`合并失败: ${result.message}`, 'error');
}
    } catch (error) {
console.error('合并序列失败:', error);
showNotification('合并序列失败', 'error');
    }
}

// 加载合并后的序列列表
async function loadMergedSequences() {
    try {
        const response = await fetch('/api/joint-sequences/merged/');
        const result = await response.json();
        
        const container = document.getElementById('mergedSequencesList');
        const containerDiv = document.getElementById('mergeExecuteContainer');
        const executeBtn = document.getElementById('executeMergedBtn');
        if (!container || !containerDiv) return;
        
        container.innerHTML = '';
        
        if (result.success && result.data && result.data.length > 0) {
            result.data.forEach(file => {
                const item = document.createElement('div');
                item.className = 'global-sequence-item';
                
                const typeColor = file.type === 'up' ? '#28a745' : '#dc3545';
                const typeText = file.type === 'up' ? 'UP' : 'DOWN';
                
                item.innerHTML = `
                    <input type="checkbox" class="merged-sequence-checkbox" value="${file.filename}" 
                           data-filename="${file.filename}"
                           onchange="updateExecuteMergedButton()">
                    <span class="global-sequence-type" style="color: ${typeColor}; font-weight: bold;">${typeText}</span>
                    <span class="global-sequence-name" style="font-weight: bold;">${file.name}</span>
                `;
                
                // 点击整个卡片也能切换复选框
                item.addEventListener('click', function(e) {
                    if (e.target.type !== 'checkbox') {
                        const checkbox = this.querySelector('.merged-sequence-checkbox');
                        if (checkbox) {
                            checkbox.checked = !checkbox.checked;
                            updateExecuteMergedButton();
                        }
                    }
                });
                
                container.appendChild(item);
            });
            containerDiv.style.display = 'grid';
            if (executeBtn) {
                updateExecuteMergedButton();
            }
        } else {
            container.innerHTML = '<p style="color: #999; font-size: 0.9em; text-align: center; grid-column: 1 / -1;">暂无合并序列</p>';
            if (result.success) {
                containerDiv.style.display = 'grid';
            } else {
                containerDiv.style.display = 'none';
            }
        }
    } catch (error) {
        console.error('加载合并序列失败:', error);
        const containerDiv = document.getElementById('mergeExecuteContainer');
        if (containerDiv) {
            containerDiv.style.display = 'none';
        }
    }
}

// 更新执行合并序列按钮状态
function updateExecuteMergedButton() {
    const checkboxes = document.querySelectorAll('.merged-sequence-checkbox:checked');
    const executeBtn = document.getElementById('executeMergedBtn');
    
    if (executeBtn) {
        executeBtn.disabled = checkboxes.length === 0;
    }
}

// 执行选中的合并序列
async function executeSelectedMergedSequences() {
    const checkboxes = document.querySelectorAll('.merged-sequence-checkbox:checked');
    if (checkboxes.length === 0) {
        showNotification('请先选择要执行的序列', 'warning');
        return;
    }
    
    const filenames = Array.from(checkboxes).map(cb => cb.dataset.filename);
    
    for (const filename of filenames) {
        await executeMergedSequence(filename);
        // 序列之间稍作延迟
        await new Promise(resolve => setTimeout(resolve, 500));
    }
}

// 执行单个合并序列
async function executeMergedSequence(filename) {
    try {
        const response = await fetch('/api/joint-sequences/execute-merged/', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                file_name: filename
            })
        });
        
        const result = await response.json();
        if (result.success) {
            showNotification(`开始执行序列: ${filename}`, 'success');
        } else {
            showNotification(`执行失败: ${result.message}`, 'error');
        }
    } catch (error) {
        console.error('执行合并序列失败:', error);
        showNotification('执行序列失败', 'error');
    }
}

// 显示合并对话框
function showGlobalMergeDialog() {
    const checkboxes = document.querySelectorAll('.global-sequence-checkbox:checked');
    if (checkboxes.length !== 2) {
        showNotification('请选择两个序列进行合并(一个左臂,一个右臂)', 'warning');
        return;
    }
    
    const selectedNames = Array.from(checkboxes).map(cb => cb.value);
    document.getElementById('selectedSequences').textContent = selectedNames.join(' + ');
    document.getElementById('mergedSequenceName').value = '';
    
    window.selectedSequenceNames = selectedNames;
    document.getElementById('mergeSequenceModal').style.display = 'block';
}

// 点击模态框背景关闭对话框
window.onclick = function(event) {
    const saveModal = document.getElementById('saveSequenceModal');
    const mergeModal = document.getElementById('mergeSequenceModal');
    const setZeroModal = document.getElementById('setZeroModal');
    if (event.target === saveModal) {
closeSaveSequenceDialog();
    }
    if (event.target === mergeModal) {
closeMergeDialog();
    }
    if (event.target === setZeroModal) {
closeSetZeroDialog();
    }
}

// 查询当前角度值
async function queryCurrentAngles(interfaceName) {
    
    try {
        console.log('queryCurrentAngles', interfaceName);
        //showNotification(`正在查询 ${interfaceName} 的当前角度值...`, 'info');
        
        const response = await fetch('/api/arm/', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                interface: interfaceName,
                action: 'queryangles'
            })
        });

        if (!response.ok) {
            console.log("errors");
            const errorText = await response.text();
            showNotification(`查询失败: HTTP ${response.status} - ${errorText}`, 'error');
            return;
        }
        
        const result = await response.json();
        console.log("result", result);
        
        if (result.success && result.data) {
            const angles = result.data.angles || {};
            const params = result.data.params || {};
            
            // 获取当前臂的电机ID列表
            const arm = devices.arms.find(a => a.interface === interfaceName);
            if (!arm) {
                showNotification('未找到机械臂设备', 'error');
                return;
            }
            
            // 构建角度字符串，格式：61: 0.000000, 62: 0.000000, ...
            const angleStrings = arm.motor_ids.map(motorID => {
                const angle = angles[motorID] !== undefined ? angles[motorID] : 0;
                return `${motorID}: ${angle.toFixed(6)}`;
            });
            
            const angleText = angleStrings.join(', ');
            
            // 填充到批量设置角度文本框
            const batchInput = document.getElementById(`batchAngles-${interfaceName}`);
            if (batchInput) {
                batchInput.value = angleText;
            }
            
            // 更新参数值显示
            if (params.loc_kp !== undefined) {
                const locKpDisplay = document.getElementById(`displayLocKp-${interfaceName}`);
                if (locKpDisplay) {
                    locKpDisplay.textContent = params.loc_kp.toFixed(2);
                }
            }
            if (params.spd_kp !== undefined) {
                const spdKpDisplay = document.getElementById(`displaySpdKp-${interfaceName}`);
                if (spdKpDisplay) {
                    spdKpDisplay.textContent = params.spd_kp.toFixed(2);
                }
            }
            if (params.spd_ki !== undefined) {
                const spdKiDisplay = document.getElementById(`displaySpdKi-${interfaceName}`);
                if (spdKiDisplay) {
                    spdKiDisplay.textContent = params.spd_ki.toFixed(2);
                }
            }
            
            showNotification(`查询成功！已填充角度值`, 'success');
        } else {
            showNotification(`查询失败: ${result.message || '未知错误'}`, 'error');
        }
    } catch (error) {
        console.error('查询当前角度失败:', error);
        showNotification(`查询失败: ${error.message}`, 'error');
    }
}

// 定期更新状态
setInterval(updateStatus, 5000);

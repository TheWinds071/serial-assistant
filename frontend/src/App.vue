<script setup lang="ts">
import { ref, onMounted, nextTick, watch, computed, reactive } from 'vue';
// 引入后端方法 (新增 OpenJLink)
import { GetSerialPorts, OpenSerial, OpenTcpClient, OpenTcpServer, OpenUdp, OpenJLink, Close as CloseConnection, SendData } from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

// --- 1. 核心状态 ---
const portList = ref<string[]>([]);
const selectedPort = ref('');
const isConnected = ref(false);

// 模式选择 (新增 JLINK)
type ConnectionMode = 'SERIAL' | 'TCP_CLIENT' | 'TCP_SERVER' | 'UDP' | 'JLINK';
const mode = ref<ConnectionMode>('SERIAL');
const showMoreModes = ref(false); // 控制更多菜单显示

// 切换模式辅助函数
const switchMode = (m: ConnectionMode) => {
  if (isConnected.value) {
    isShaking.value = true;
    setTimeout(() => { isShaking.value = false; }, 500);
    return;
  }
  mode.value = m;
  showMoreModes.value = false;
};

// 震动动画状态
const isShaking = ref(false);

// Serial 参数
const baudRate = ref(115200);
const dataBits = ref(8);
const stopBits = ref(1);
const parity = ref('None');
const baudOptions = [9600, 19200, 38400, 57600, 115200, 921600];

// Network 参数
const netIp = ref('127.0.0.1');
const netPort = ref('43211');
const udpLocalPort = ref('8081');

// J-Link 参数 (新增)
const jlinkChip = ref('STM32H750VB');
const jlinkSpeed = ref(8000);
const jlinkInterface = ref('SWD');

// --- 2. 数据处理 ---
const receivedData = ref<string>('');
const rawDataBuffer = ref<number[]>([]);
const sendInput = ref('');
// 默认关闭 Hex 显示
const showHex = ref(false);
// Hex 发送状态
const hexSend = ref(false);

// 行尾符配置
const lineEndingMode = ref<'NONE' | 'LF' | 'CRLF'>('NONE');
const showEolDropdown = ref(false);

const eolOptions = [
  { label: 'None', value: 'NONE' },
  { label: '\\n (LF)', value: 'LF' },
  { label: '\\r\\n (CRLF)', value: 'CRLF' }
];

const currentEolLabel = computed(() =>
    eolOptions.find(o => o.value === lineEndingMode.value)?.label || 'None'
);

const selectEol = (val: 'NONE' | 'LF' | 'CRLF') => {
  lineEndingMode.value = val;
  showEolDropdown.value = false;
};

const autoScroll = ref(true);
const logWindowRef = ref<HTMLElement | null>(null);
const rxCount = ref(0);
const txCount = ref(0);

// --- 3. UI 状态 (主题 & 弹窗) ---
const showThemePanel = ref(false);
// 定义主题类型以避免索引错误
type ThemeType = {
  bgMain: string;
  bgSide: string;
  primary: string;
  textMain: string;
  textSub: string;
  error: string;
};

const defaultTheme: ThemeType = {
  bgMain: '#F2F1ED',
  bgSide: '#EBEAE6',
  primary: '#7A8B99',
  textMain: '#5C5C5C',
  textSub: '#888888',
  error: '#CF6679'
};
const theme = reactive({ ...defaultTheme });

const cssVars = computed(() => ({
  '--bg-main': theme.bgMain,
  '--bg-side': theme.bgSide,
  '--col-primary': theme.primary,
  '--text-main': theme.textMain,
  '--text-sub': theme.textSub,
  '--col-error': theme.error
}));

const resetTheme = () => Object.assign(theme, defaultTheme);

// 辅助：获取友好的显示名称
const getThemeLabel = (key: string) => {
  const map: Record<string, string> = {
    bgMain: '主背景',
    bgSide: '侧边栏',
    primary: '主色调',
    textMain: '主要文字',
    textSub: '次要文字',
    error: '错误色'
  };
  return map[key] || key;
};

// 自定义弹窗状态
const modal = reactive({
  show: false,
  title: '',
  message: '',
  type: 'error' as 'error' | 'info' | 'success'
});

const showModal = (title: string, message: string, type: 'error' | 'info' | 'success' = 'error') => {
  modal.title = title;
  modal.message = message;
  modal.type = type;
  modal.show = true;
};

const closeModal = () => {
  modal.show = false;
};

// --- 4. 生命周期 ---
onMounted(async () => {
  await refreshPorts();

  // 数据接收监听
  EventsOn("serial-data", (data: any) => {
    let bytes: number[] = [];
    if (typeof data === 'string') {
      try {
        bytes = base64ToBytes(data);
      } catch (e) {
        console.error("Base64 decode error:", e);
        return;
      }
    } else if (Array.isArray(data)) {
      bytes = data;
    }

    if (bytes && bytes.length > 0) {
      rawDataBuffer.value.push(...bytes);
      rxCount.value += bytes.length;
      receivedData.value += formatData(bytes, showHex.value);
      scrollToBottom();
    }
  });

  EventsOn("serial-error", (err) => {
    console.error("Connection error:", err);
    isConnected.value = false;
    showModal("连接断开", String(err), 'error');
  });

  EventsOn("sys-msg", (msg) => {
    console.log("Sys Msg:", msg);
  });
});

const base64ToBytes = (base64: string): number[] => {
  const binaryString = window.atob(base64);
  const len = binaryString.length;
  const bytes = new Array(len);
  for (let i = 0; i < len; i++) bytes[i] = binaryString.charCodeAt(i);
  return bytes;
};

const refreshPorts = async () => {
  try {
    portList.value = await GetSerialPorts();
    if (portList.value.length > 0 && !selectedPort.value) selectedPort.value = portList.value[0];
  } catch (e) { console.error(e); }
};

const toggleConnection = async () => {
  if (isConnected.value) {
    await CloseConnection();
    isConnected.value = false;
  } else {
    let res = "";
    if (mode.value === 'SERIAL') {
      if (!selectedPort.value) return;
      res = await OpenSerial(selectedPort.value, Number(baudRate.value), Number(dataBits.value), Number(stopBits.value), parity.value);
    } else if (mode.value === 'JLINK') {
      if (!jlinkChip.value) return;
      res = await OpenJLink(jlinkChip.value, Number(jlinkSpeed.value), jlinkInterface.value);
    } else if (mode.value === 'TCP_CLIENT') {
      if (!netIp.value || !netPort.value) return;
      res = await OpenTcpClient(netIp.value, netPort.value);
    } else if (mode.value === 'TCP_SERVER') {
      if (!netPort.value) return;
      res = await OpenTcpServer(netPort.value);
    } else if (mode.value === 'UDP') {
      if (!udpLocalPort.value) return;
      res = await OpenUdp(udpLocalPort.value, netIp.value, netPort.value);
    }

    if (res === "Success") {
      isConnected.value = true;
    } else {
      showModal("连接失败", res, 'error');
    }
  }
};

const handleSend = async () => {
  if (!sendInput.value) return;

  let dataToSend = "";

  if (hexSend.value) {
    const cleanInput = sendInput.value.replace(/\s+/g, '');
    if (!/^[0-9A-Fa-f]*$/.test(cleanInput)) {
      showModal("格式错误", "Hex 字符串包含非法字符 (0-9, A-F)", 'error');
      return;
    }
    if (cleanInput.length % 2 !== 0) {
      showModal("格式错误", "Hex 字符串长度必须为偶数 (例如: AA BB)", 'error');
      return;
    }
    for (let i = 0; i < cleanInput.length; i += 2) {
      const hexPair = cleanInput.substring(i, i + 2);
      dataToSend += String.fromCharCode(parseInt(hexPair, 16));
    }
  } else {
    dataToSend = sendInput.value;
    if (lineEndingMode.value === 'LF') {
      dataToSend += "\n";
    } else if (lineEndingMode.value === 'CRLF') {
      dataToSend += "\r\n";
    }
  }

  const res = await SendData(dataToSend);

  if(res === 'Sent') {
    txCount.value += dataToSend.length;
  } else {
    showModal("发送失败", res, 'error');
  }
};

const clearReceive = () => {
  receivedData.value = '';
  rawDataBuffer.value = [];
  rxCount.value = 0;
};

const decoder = new TextDecoder('utf-8');
const formatData = (bytes: number[], isHex: boolean): string => {
  if (isHex) {
    return bytes.map(b => b.toString(16).padStart(2, '0').toUpperCase()).join(' ') + ' ';
  } else {
    return decoder.decode(new Uint8Array(bytes), { stream: true });
  }
};

watch(showHex, () => {
  receivedData.value = formatData(rawDataBuffer.value, showHex.value);
});

const scrollToBottom = () => {
  if (!autoScroll.value || !logWindowRef.value) return;
  nextTick(() => {
    if(logWindowRef.value) logWindowRef.value.scrollTop = logWindowRef.value.scrollHeight;
  });
};
</script>

<template>
  <div :style="cssVars as any" class="flex h-screen w-screen bg-[var(--bg-main)] text-[var(--text-main)] font-sans overflow-hidden select-none transition-colors duration-300">

    <!-- 侧边栏 -->
    <div class="w-72 bg-[var(--bg-side)] flex flex-col shrink-0 border-r border-black/5 transition-colors duration-300 relative">
      <div class="h-14 flex items-center justify-between px-4 border-b border-black/5">
        <span class="font-bold text-lg tracking-widest text-[var(--col-primary)]">SERIAL MATE</span>
        <button @click="showThemePanel = !showThemePanel" class="p-1.5 rounded-md hover:bg-black/5 text-[var(--text-sub)] transition-colors" :class="{'bg-black/5': showThemePanel}">
          <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="13.5" cy="6.5" r=".5"></circle><circle cx="17.5" cy="10.5" r=".5"></circle><circle cx="8.5" cy="7.5" r=".5"></circle><circle cx="6.5" cy="12.5" r=".5"></circle><path d="M12 2C6.5 2 2 6.5 2 12s4.5 10 10 10c.926 0 1.648-.746 1.648-1.688 0-.437-.18-.835-.437-1.125-.29-.289-.438-.652-.438-1.125a1.64 1.64 0 0 1 1.668-1.668h1.996c3.051 0 5.555-2.503 5.555-5.554C21.965 6.012 17.461 2 12 2z"></path></svg>
        </button>
      </div>

      <!-- 主题面板 (已修复：添加了颜色输入控件) -->
      <Transition name="slide-down">
        <div v-if="showThemePanel" class="absolute top-14 left-0 w-full bg-white/95 backdrop-blur-md p-4 shadow-xl border-b border-black/5 z-20 flex flex-col gap-3">
          <div class="flex justify-between items-center text-xs font-bold text-[var(--text-sub)] mb-1">
            <span>自定义配色</span>
            <button @click="resetTheme" class="hover:text-[var(--col-primary)] transition-colors">重置默认</button>
          </div>

          <div class="grid grid-cols-2 gap-3">
            <div v-for="(val, key) in theme" :key="key" class="flex flex-col gap-1">
              <label class="text-[10px] font-bold text-[var(--text-sub)] uppercase tracking-wide">{{ getThemeLabel(key.toString()) }}</label>
              <div class="flex items-center gap-2 bg-black/5 rounded p-1 pl-2">
                <input type="color" v-model="theme[key as keyof ThemeType]" class="w-5 h-5 rounded cursor-pointer border-none bg-transparent p-0 shrink-0">
                <input type="text" v-model="theme[key as keyof ThemeType]" class="w-full bg-transparent border-none text-[10px] font-mono text-[var(--text-main)] focus:outline-none uppercase">
              </div>
            </div>
          </div>
        </div>
      </Transition>

      <div class="flex-1 overflow-y-auto p-5 space-y-5 custom-scrollbar relative z-10">

        <!-- 模式切换区域 -->
        <div class="flex gap-2 transition-transform duration-100" :class="{ 'shake-anim': isShaking }">
          <!-- 常用模式平铺 -->
          <div class="flex-1 bg-white/40 p-1 rounded-lg shadow-sm border border-black/5 flex gap-1 text-[10px] font-bold">
            <button @click="switchMode('SERIAL')"
                    :class="{'bg-white text-[var(--col-primary)] shadow-sm': mode==='SERIAL', 'text-[var(--text-sub)] hover:bg-black/5': mode!=='SERIAL'}"
                    class="flex-1 py-1.5 rounded transition-all">SERIAL</button>
            <button @click="switchMode('JLINK')"
                    :class="{'bg-white text-[var(--col-primary)] shadow-sm': mode==='JLINK', 'text-[var(--text-sub)] hover:bg-black/5': mode!=='JLINK'}"
                    class="flex-1 py-1.5 rounded transition-all">J-LINK</button>
            <button @click="switchMode('TCP_CLIENT')"
                    :class="{'bg-white text-[var(--col-primary)] shadow-sm': mode==='TCP_CLIENT', 'text-[var(--text-sub)] hover:bg-black/5': mode!=='TCP_CLIENT'}"
                    class="flex-1 py-1.5 rounded transition-all">TCP-C</button>
          </div>

          <!-- 更多模式汉堡按钮 -->
          <div class="relative">
            <button @click="showMoreModes = !showMoreModes"
                    class="h-full px-2.5 bg-white/40 hover:bg-white/60 rounded-lg shadow-sm border border-black/5 flex items-center justify-center text-[var(--text-sub)] transition-all z-50 relative"
                    :class="{'bg-white text-[var(--col-primary)]': showMoreModes || (mode !== 'SERIAL' && mode !== 'JLINK' && mode !== 'TCP_CLIENT')}">
              <svg class="w-4 h-4 overflow-visible" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <line x1="3" y1="6" x2="21" y2="6" class="transition-all duration-300 origin-[12px_12px]" :class="showMoreModes ? 'translate-y-[6px] rotate-45' : ''"></line>
                <line x1="3" y1="12" x2="21" y2="12" class="transition-all duration-300" :class="showMoreModes ? 'opacity-0' : ''"></line>
                <line x1="3" y1="18" x2="21" y2="18" class="transition-all duration-300 origin-[12px_12px]" :class="showMoreModes ? '-translate-y-[6px] -rotate-45' : ''"></line>
              </svg>
            </button>

            <!-- 点击遮罩 -->
            <div v-if="showMoreModes" @click="showMoreModes = false" class="fixed inset-0 z-40 cursor-default"></div>

            <!-- 下拉菜单 (已修复：添加了下拉动画 dropdown-fade) -->
            <Transition name="dropdown-fade">
              <div v-if="showMoreModes" class="absolute top-full right-0 mt-2 w-32 bg-white/95 backdrop-blur-xl shadow-xl border border-white/50 rounded-lg p-1.5 z-50 flex flex-col gap-1 ring-1 ring-black/5 origin-top-right">
                <button @click="switchMode('TCP_SERVER')"
                        class="flex items-center justify-between w-full px-3 py-2 text-[11px] font-bold rounded-md transition-all text-left"
                        :class="mode === 'TCP_SERVER' ? 'bg-[var(--col-primary)] text-white shadow-sm' : 'text-[var(--text-main)] hover:bg-black/5'">
                  <span>TCP SERVER</span>
                  <span v-if="mode === 'TCP_SERVER'">✓</span>
                </button>
                <button @click="switchMode('UDP')"
                        class="flex items-center justify-between w-full px-3 py-2 text-[11px] font-bold rounded-md transition-all text-left"
                        :class="mode === 'UDP' ? 'bg-[var(--col-primary)] text-white shadow-sm' : 'text-[var(--text-main)] hover:bg-black/5'">
                  <span>UDP</span>
                  <span v-if="mode === 'UDP'">✓</span>
                </button>
              </div>
            </Transition>
          </div>
        </div>

        <!-- 设置面板主体 -->
        <div class="bg-white/40 p-3 rounded-lg shadow-sm border border-black/5 space-y-3 overflow-hidden">
          <div class="text-xs font-bold text-[var(--text-sub)] opacity-70 uppercase tracking-wider mb-1 flex justify-between items-center">
            <span>{{ mode.replace('_', ' ') }} Settings</span>
            <span v-if="mode !== 'SERIAL' && mode !== 'JLINK' && mode !== 'TCP_CLIENT'" class="text-[10px] bg-[var(--col-primary)] text-white px-1.5 py-0.5 rounded-full">More</span>
          </div>

          <Transition name="fade" mode="out-in">
            <!-- Serial Settings -->
            <div v-if="mode === 'SERIAL'" key="SERIAL" class="space-y-3">
              <div class="control-group"><label>端口</label><div class="relative flex-1"><select v-model="selectedPort" @click="refreshPorts" class="morandi-input" :disabled="isConnected"><option v-for="p in portList" :key="p" :value="p">{{ p }}</option></select></div></div>
              <div class="control-group"><label>波特率</label><div class="relative flex-1"><input type="number" v-model="baudRate" list="baud-list" class="morandi-input" placeholder="Custom" :disabled="isConnected"><datalist id="baud-list"><option v-for="b in baudOptions" :key="b" :value="b"></option></datalist></div></div>
              <div class="control-group"><label>数据位</label><select v-model="dataBits" class="morandi-input flex-1" :disabled="isConnected"><option value="8">8</option><option value="7">7</option><option value="6">6</option><option value="5">5</option></select></div>
              <div class="control-group"><label>校验位</label><select v-model="parity" class="morandi-input flex-1" :disabled="isConnected"><option value="None">None</option><option value="Odd">Odd</option><option value="Even">Even</option><option value="Mark">Mark</option><option value="Space">Space</option></select></div>
              <div class="control-group"><label>停止位</label><select v-model="stopBits" class="morandi-input flex-1" :disabled="isConnected"><option value="1">1</option><option value="1.5">1.5</option><option value="2">2</option></select></div>
            </div>

            <!-- J-LINK Settings -->
            <div v-else-if="mode === 'JLINK'" key="JLINK" class="space-y-3">
              <div class="control-group"><label>Chip</label><input type="text" v-model="jlinkChip" class="morandi-input" placeholder="e.g. STM32F407VE" :disabled="isConnected"></div>
              <div class="control-group"><label>Interface</label><select v-model="jlinkInterface" class="morandi-input flex-1" :disabled="isConnected"><option value="SWD">SWD</option><option value="JTAG">JTAG</option></select></div>
              <div class="control-group"><label>Speed</label><input type="number" v-model="jlinkSpeed" class="morandi-input" placeholder="4000" :disabled="isConnected"></div>
            </div>

            <!-- TCP Client Settings -->
            <div v-else-if="mode === 'TCP_CLIENT'" key="TCP_CLIENT" class="space-y-3">
              <div class="control-group"><label>IP</label><input type="text" v-model="netIp" class="morandi-input" placeholder="127.0.0.1" :disabled="isConnected"></div>
              <div class="control-group"><label>Port</label><input type="text" v-model="netPort" class="morandi-input" placeholder="43211" :disabled="isConnected"></div>
            </div>

            <!-- TCP Server Settings -->
            <div v-else-if="mode === 'TCP_SERVER'" key="TCP_SERVER" class="space-y-3">
              <div class="control-group"><label>Local Port</label><input type="text" v-model="netPort" class="morandi-input" placeholder="8080" :disabled="isConnected"></div>
            </div>

            <!-- UDP Settings -->
            <div v-else-if="mode === 'UDP'" key="UDP" class="space-y-3">
              <div class="control-group"><label>Local Port</label><input type="text" v-model="udpLocalPort" class="morandi-input" placeholder="8081" :disabled="isConnected"></div>
              <div class="my-2 border-t border-black/5"></div>
              <div class="control-group"><label>Target IP</label><input type="text" v-model="netIp" class="morandi-input" placeholder="127.0.0.1" :disabled="isConnected"></div>
              <div class="control-group"><label>Target Port</label><input type="text" v-model="netPort" class="morandi-input" placeholder="8080" :disabled="isConnected"></div>
            </div>
          </Transition>
        </div>

        <button @click="toggleConnection" class="w-full py-2.5 rounded-lg font-medium text-white transition-all duration-300 transform active:scale-[0.98] shadow-sm flex items-center justify-center space-x-2 bg-[var(--col-primary)] hover:opacity-90">
          <div v-if="!isConnected" class="w-2 h-2 rounded-full bg-white animate-pulse"></div>
          <span>{{ isConnected ? '断开' : '连接' }}</span>
        </button>

        <div class="space-y-2 pt-2">
          <div class="text-xs font-bold text-[var(--text-sub)] opacity-70 uppercase tracking-wider">Display</div>
          <label class="flex items-center space-x-2 cursor-pointer hover:text-[var(--col-primary)] transition-colors">
            <input type="checkbox" v-model="showHex" class="accent-[var(--col-primary)] w-4 h-4">
            <span class="text-sm">Hex 显示</span>
          </label>
          <label class="flex items-center space-x-2 cursor-pointer hover:text-[var(--col-primary)] transition-colors">
            <input type="checkbox" v-model="autoScroll" class="accent-[var(--col-primary)] w-4 h-4">
            <span class="text-sm">自动滚屏</span>
          </label>
        </div>
      </div>
    </div>

    <!-- 右侧主区域 (RX/TX) -->
    <div class="flex-1 flex flex-col min-w-0 p-4 gap-4 transition-colors duration-300">
      <div class="flex-1 bg-white/60 rounded-xl shadow-[0_2px_12px_-4px_rgba(0,0,0,0.08)] border border-black/5 flex flex-col overflow-hidden relative backdrop-blur-sm">
        <div class="h-10 px-4 flex items-center justify-between bg-black/[0.02] border-b border-black/5">
          <div class="flex items-center space-x-2">
            <span class="text-xs font-bold text-[var(--col-primary)] tracking-wider">RX MONITOR</span>
            <span class="text-[10px] text-[var(--text-sub)] bg-black/5 px-1.5 py-0.5 rounded-md">{{ rxCount }} Bytes</span>
          </div>
          <button @click="clearReceive" title="清空" class="group flex items-center justify-center w-7 h-7 rounded hover:bg-white hover:shadow-sm text-[var(--text-sub)] hover:text-[var(--col-primary)] transition-all">
            <svg class="w-4 h-4 broom-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 6L7.5 16.5"></path><path d="M19.5 4.5L16.5 7.5"></path><path d="M2 22L4.5 19.5"></path><path d="M9.5 12.5C7.5 14.5 6 15 5 16C4 17 3 17 3 17C3 18 4 19C5 20 5 20 5 20C5 20 6 20 7 19C8 18 8.5 16.5 10.5 14.5L18 7"></path></svg>
          </button>
        </div>
        <textarea ref="logWindowRef" readonly class="flex-1 w-full p-4 font-mono text-sm bg-transparent resize-none outline-none custom-scrollbar leading-relaxed text-[var(--text-main)]" :value="receivedData"></textarea>
      </div>

      <div class="h-40 bg-white/60 rounded-xl shadow-[0_2px_12px_-4px_rgba(0,0,0,0.08)] border border-black/5 flex flex-col overflow-hidden backdrop-blur-sm">
        <div class="h-9 px-4 flex items-center justify-between bg-black/[0.02] border-b border-black/5">
          <div class="flex items-center space-x-4">
            <span class="text-xs font-bold text-[var(--text-sub)] tracking-wider">TX EDITOR</span>
            <div class="flex items-center gap-3">
              <label class="flex items-center space-x-1.5 cursor-pointer hover:text-[var(--col-primary)] transition-colors select-none">
                <input type="checkbox" v-model="hexSend" class="accent-[var(--col-primary)] w-3.5 h-3.5 rounded-sm">
                <span class="text-[11px] font-bold opacity-70">Hex Send</span>
              </label>
              <div class="w-[1px] h-3 bg-black/10"></div>
              <div class="relative z-10" :class="{'opacity-50 pointer-events-none': hexSend}">
                <button
                    @click="showEolDropdown = !showEolDropdown"
                    class="flex items-center space-x-1.5 bg-black/5 hover:bg-black/10 transition-all px-2.5 rounded-md border border-transparent focus:border-black/5 outline-none h-7"
                    :class="{'text-[var(--col-primary)] bg-[var(--col-primary)]/10 border-[var(--col-primary)]/20': showEolDropdown}"
                >
                  <div class="flex items-baseline space-x-1 translate-y-[0.5px]">
                    <span class="text-[11px] font-bold opacity-70 leading-tight">EOL:</span>
                    <span class="text-[11px] font-mono font-medium min-w-[30px] text-center leading-tight">{{ currentEolLabel }}</span>
                  </div>
                  <svg class="w-3 h-3 opacity-50 transform transition-transform duration-200" :class="{'rotate-180': showEolDropdown}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"></polyline></svg>
                </button>
                <div v-if="showEolDropdown" @click="showEolDropdown = false" class="fixed inset-0 z-0 cursor-default"></div>
                <Transition name="slide-fade">
                  <div v-if="showEolDropdown" class="absolute top-full right-0 mt-1.5 w-32 bg-white/80 backdrop-blur-xl shadow-[0_4px_16px_-4px_rgba(0,0,0,0.1)] border border-white/50 rounded-lg p-1 z-50 flex flex-col overflow-hidden select-none ring-1 ring-black/5">
                    <button v-for="opt in eolOptions" :key="opt.value" @click="selectEol(opt.value as any)" class="relative flex items-center justify-between w-full px-3 py-2 text-[11px] font-mono rounded-md transition-all outline-none" :class="lineEndingMode === opt.value ? 'bg-[var(--col-primary)] text-white shadow-sm font-medium' : 'text-[var(--text-main)] hover:bg-black/5'">
                      <span>{{ opt.label }}</span>
                      <span v-if="lineEndingMode === opt.value" class="text-[10px] font-bold">✓</span>
                    </button>
                  </div>
                </Transition>
              </div>
            </div>
          </div>
        </div>

        <div class="flex-1 flex p-3 gap-3">
          <textarea v-model="sendInput" class="flex-1 bg-white/50 border border-transparent focus:border-[var(--col-primary)]/30 rounded-lg p-3 font-mono text-sm text-[var(--text-main)] focus:bg-white transition-all outline-none resize-none placeholder-[var(--text-sub)]/50" :placeholder="hexSend ? 'Input Hex (e.g., AA BB CC)...' : 'Input data to send...'" @keydown.enter.ctrl.prevent="handleSend"></textarea>
          <div class="flex flex-col gap-2 w-20">
            <button @click="handleSend" class="flex-1 bg-[var(--col-primary)] hover:opacity-90 text-white rounded-lg shadow-sm transition-all flex flex-col items-center justify-center active:scale-95"><span class="text-xs font-bold tracking-widest">SEND</span></button>
            <button @click="sendInput=''" class="h-8 bg-black/5 text-[var(--text-sub)] hover:bg-black/10 rounded-lg text-xs">CLR</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 自定义弹窗 (Modal) -->
    <Transition name="modal-fade">
      <div v-if="modal.show" class="fixed inset-0 z-50 flex items-center justify-center bg-black/20 backdrop-blur-[2px] transition-all">
        <div class="bg-white/95 rounded-xl shadow-2xl border border-white/50 w-[420px] max-w-[90%] overflow-hidden transform transition-all scale-100 flex flex-col" @click.stop>
          <div class="h-10 flex items-center justify-between px-4 bg-black/[0.03] border-b border-black/5">
            <div class="flex items-center gap-2">
              <svg v-if="modal.type === 'error'" class="w-4 h-4 text-[var(--col-error)]" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>
              <svg v-else class="w-4 h-4 text-[var(--col-primary)]" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="16" x2="12" y2="12"></line><line x1="12" y1="8" x2="12.01" y2="8"></line></svg>
              <span class="text-xs font-bold tracking-wide" :class="modal.type === 'error' ? 'text-[var(--col-error)]' : 'text-[var(--col-primary)]'">{{ modal.title }}</span>
            </div>
            <button @click="closeModal" class="text-[var(--text-sub)] hover:text-[var(--text-main)] transition-colors"><svg class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg></button>
          </div>
          <div class="p-5"><p class="text-sm text-[var(--text-main)] leading-relaxed font-medium mb-1 opacity-90 break-words font-mono bg-black/5 p-3 rounded-lg border border-black/5 text-[11px] max-h-40 overflow-y-auto custom-scrollbar">{{ modal.message }}</p></div>
          <div class="px-5 pb-5 flex justify-end"><button @click="closeModal" class="bg-[var(--col-primary)] text-white text-xs font-bold px-6 py-2 rounded-lg hover:opacity-90 active:scale-95 transition-all shadow-sm">确 定</button></div>
        </div>
      </div>
    </Transition>

  </div>
</template>

<style scoped>
.control-group { display: flex; align-items: center; gap: 0.5rem; }
.control-group label { width: 60px; text-align: right; font-size: 0.75rem; color: var(--text-sub); }
.morandi-input { width: 100%; background-color: rgba(255, 255, 255, 0.6); border: 1px solid rgba(0, 0, 0, 0.1); color: var(--text-main); padding: 0.25rem 0.5rem; font-size: 0.8rem; border-radius: 0.375rem; outline: none; transition: all 0.2s; }
.morandi-input:focus { background-color: #fff; border-color: var(--col-primary); }
.morandi-input:disabled { opacity: 0.6; cursor: not-allowed; }
.custom-scrollbar::-webkit-scrollbar { width: 6px; height: 6px; }
.custom-scrollbar::-webkit-scrollbar-track { background: transparent; }
.custom-scrollbar::-webkit-scrollbar-thumb { background: rgba(0,0,0,0.15); border-radius: 3px; }
.custom-scrollbar::-webkit-scrollbar-thumb:hover { background: var(--col-primary); }

.fade-enter-active,
.fade-leave-active { transition: opacity 0.2s ease, transform 0.2s ease; }
.fade-enter-from,
.fade-leave-to { opacity: 0; transform: translateY(5px); }

.slide-fade-enter-active { transition: all 0.2s ease-out; }
.slide-fade-leave-active { transition: all 0.15s cubic-bezier(1, 0.5, 0.8, 1); }
.slide-fade-enter-from,
.slide-fade-leave-to { transform: translateY(-5px); opacity: 0; }

/* 主题面板下拉动画 */
.slide-down-enter-active { transition: all 0.3s cubic-bezier(0.16, 1, 0.3, 1); }
.slide-down-leave-active { transition: all 0.2s ease-in; }
.slide-down-enter-from,
.slide-down-leave-to { transform: translateY(-10px); opacity: 0; }

/* 汉堡菜单下拉动画 */
.dropdown-fade-enter-active { transition: all 0.2s cubic-bezier(0.16, 1, 0.3, 1); }
.dropdown-fade-leave-active { transition: all 0.15s ease-in; }
.dropdown-fade-enter-from,
.dropdown-fade-leave-to { transform: scale(0.95) translateY(-5px); opacity: 0; }

.modal-fade-enter-active,
.modal-fade-leave-active { transition: all 0.2s ease-out; }
.modal-fade-enter-from,
.modal-fade-leave-to { opacity: 0; }
.modal-fade-enter-from .bg-white\/95,
.modal-fade-leave-to .bg-white\/95 { transform: scale(0.95); opacity: 0; }

@keyframes shake-x {
  0%, 100% { transform: translateX(0); }
  10%, 30%, 50%, 70%, 90% { transform: translateX(-4px); }
  20%, 40%, 60%, 80% { transform: translateX(4px); }
}
.shake-anim { animation: shake-x 0.4s cubic-bezier(0.36, 0.07, 0.19, 0.97) both; border-color: rgba(239, 68, 68, 0.5); }

@keyframes broom-sweep {
  0%, 100% { transform: rotate(0deg) translateX(0); }
  25% { transform: rotate(-8deg) translateX(-2px); }
  50% { transform: rotate(8deg) translateX(2px); }
  75% { transform: rotate(-8deg) translateX(-2px); }
}
.broom-icon {
  transition: transform 0.2s ease;
}
.group:hover .broom-icon {
  animation: broom-sweep 0.6s ease-in-out;
}
</style>
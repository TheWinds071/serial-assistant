<script setup lang="ts">
import { ref, onMounted, nextTick, watch, computed, reactive } from 'vue';
import { GetSerialPorts, OpenSerial, CloseSerial, SendData } from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

// --- 1. 核心状态 ---
const portList = ref<string[]>([]);
const selectedPort = ref('');
const isConnected = ref(false);
const baudRate = ref(115200);
const dataBits = ref(8);
const stopBits = ref(1);
const parity = ref('None');
const baudOptions = [9600, 19200, 38400, 57600, 115200, 921600];

// --- 2. 数据处理 ---
const receivedData = ref<string>('');
const rawDataBuffer = ref<number[]>([]);
const sendInput = ref('');
const showHex = ref(true);
const sendHex = ref(false);
const autoScroll = ref(true);
const logWindowRef = ref<HTMLElement | null>(null);
const rxCount = ref(0);
const txCount = ref(0);

// --- 3. 主题与配色逻辑 (简化版) ---
const showThemePanel = ref(false);

// 默认莫兰迪配色 (精简为4个变量)
const defaultTheme = {
  bgMain: '#F2F1ED',       // 背景暖白
  bgSide: '#EBEAE6',       // 侧栏浅灰
  primary: '#7A8B99',      // 主色(雾霾蓝) - 核心颜色
  textMain: '#5C5C5C',     // 主要文字
  textSub: '#888888',      // 次要文字/标签
};

const theme = reactive({ ...defaultTheme });

// 计算 CSS 变量，绑定到根节点
const cssVars = computed(() => ({
  '--bg-main': theme.bgMain,
  '--bg-side': theme.bgSide,
  '--col-primary': theme.primary,
  '--text-main': theme.textMain,
  '--text-sub': theme.textSub,
}));

const resetTheme = () => {
  Object.assign(theme, defaultTheme);
};

// --- 4. 生命周期与方法 ---
onMounted(async () => {
  await refreshPorts();

  // 注意：这里 data 类型设为 any，因为可能是 string (Base64) 也可能是 array
  EventsOn("serial-data", (data: any) => {
    let bytes: number[] = [];

    // 1. 核心修复：如果是 Base64 字符串，先解码
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

    // 2. 正常的显示逻辑
    if (bytes && bytes.length > 0) {
      rawDataBuffer.value.push(...bytes);
      rxCount.value += bytes.length;
      // 使用之前修正过的 formatData (含 TextDecoder 或 Hex 处理)
      receivedData.value += formatData(bytes, showHex.value);
      scrollToBottom();
    }
  });

  EventsOn("serial-error", (err) => {
    alert("Serial Error: " + err);
    isConnected.value = false;
  });
});

// 辅助函数：Wails 传来的 []byte 会变成 Base64 字符串，需要转回数字数组
const base64ToBytes = (base64: string): number[] => {
  const binaryString = window.atob(base64);
  const len = binaryString.length;
  const bytes = new Array(len);
  for (let i = 0; i < len; i++) {
    bytes[i] = binaryString.charCodeAt(i);
  }
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
    await CloseSerial();
    isConnected.value = false;
  } else {
    if (!selectedPort.value) return;
    const res = await OpenSerial(selectedPort.value, Number(baudRate.value), Number(dataBits.value), Number(stopBits.value), parity.value);
    if (res === "Success") isConnected.value = true;
    else alert(res);
  }
};

const handleSend = async () => {
  if (!sendInput.value) return;
  const res = await SendData(sendInput.value);
  if(res === 'Sent') txCount.value += sendInput.value.length;
};

const clearReceive = () => {
  receivedData.value = '';
  rawDataBuffer.value = [];
  rxCount.value = 0;
};

const decoder = new TextDecoder('utf-8'); // 创建解码器实例

const formatData = (bytes: number[], isHex: boolean): string => {
  if (isHex) {
    return bytes.map(b => b.toString(16).padStart(2, '0').toUpperCase()).join(' ') + ' ';
  } else {
    // 使用 TextDecoder 解析字节流，支持中文和标准 UTF-8
    // 注意：这里需要把 number[] 转为 Uint8Array
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
  <div :style="cssVars" class="flex h-screen w-screen bg-[var(--bg-main)] text-[var(--text-main)] font-sans overflow-hidden select-none transition-colors duration-300">

    <div class="w-72 bg-[var(--bg-side)] flex flex-col shrink-0 border-r border-black/5 transition-colors duration-300 relative">

      <div class="h-14 flex items-center justify-between px-4 border-b border-black/5">
        <span class="font-bold text-lg tracking-widest text-[var(--col-primary)]">SERIAL MATE</span>
        <button @click="showThemePanel = !showThemePanel" class="p-1.5 rounded-md hover:bg-black/5 text-[var(--text-sub)] transition-colors" title="自定义主题">
          <svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="13.5" cy="6.5" r=".5"></circle><circle cx="17.5" cy="10.5" r=".5"></circle><circle cx="8.5" cy="7.5" r=".5"></circle><circle cx="6.5" cy="12.5" r=".5"></circle><path d="M12 2C6.5 2 2 6.5 2 12s4.5 10 10 10c.926 0 1.648-.746 1.648-1.688 0-.437-.18-.835-.437-1.125-.29-.289-.438-.652-.438-1.125a1.64 1.64 0 0 1 1.668-1.668h1.996c3.051 0 5.555-2.503 5.555-5.554C21.965 6.012 17.461 2 12 2z"></path></svg>
        </button>
      </div>

      <div v-if="showThemePanel" class="absolute top-14 left-0 w-full bg-white/90 backdrop-blur-md p-4 shadow-lg border-b border-black/5 z-20 space-y-3">
        <div class="flex justify-between items-center text-xs font-bold text-[var(--text-sub)]">
          <span>自定义配色 (THEME)</span>
          <button @click="resetTheme" class="hover:text-[var(--col-primary)]">重置</button>
        </div>
        <div class="grid grid-cols-2 gap-2 text-xs text-[var(--text-sub)]">
          <div class="flex items-center justify-between">背景 <input type="color" v-model="theme.bgMain" class="w-6 h-6 rounded cursor-pointer border-none bg-transparent"></div>
          <div class="flex items-center justify-between">侧栏 <input type="color" v-model="theme.bgSide" class="w-6 h-6 rounded cursor-pointer border-none bg-transparent"></div>
          <div class="flex items-center justify-between font-bold text-[var(--col-primary)]">主色 <input type="color" v-model="theme.primary" class="w-6 h-6 rounded cursor-pointer border-none bg-transparent"></div>
          <div class="flex items-center justify-between">文字 <input type="color" v-model="theme.textMain" class="w-6 h-6 rounded cursor-pointer border-none bg-transparent"></div>
        </div>
      </div>

      <div class="flex-1 overflow-y-auto p-5 space-y-5 custom-scrollbar">
        <div class="bg-white/40 p-3 rounded-lg shadow-sm border border-black/5 space-y-3">
          <div class="text-xs font-bold text-[var(--text-sub)] opacity-70 uppercase tracking-wider mb-1">Port Settings</div>

          <div class="control-group">
            <label>端口</label>
            <div class="relative flex-1">
              <select v-model="selectedPort" @click="refreshPorts" class="morandi-input">
                <option v-for="p in portList" :key="p" :value="p">{{ p }}</option>
              </select>
            </div>
          </div>

          <div class="control-group">
            <label>波特率</label>
            <div class="relative flex-1">
              <input type="number" v-model="baudRate" list="baud-list" class="morandi-input" placeholder="Custom">
              <datalist id="baud-list">
                <option v-for="b in baudOptions" :key="b" :value="b"></option>
              </datalist>
            </div>
          </div>
          <div class="control-group">
            <label>数据位</label>
            <select v-model="dataBits" class="morandi-input flex-1">
              <option :value="5">5</option><option :value="6">6</option><option :value="7">7</option><option :value="8">8</option>
            </select>
          </div>
          <div class="control-group">
            <label>校验位</label>
            <select v-model="parity" class="morandi-input flex-1">
              <option value="None">None</option><option value="Odd">Odd</option><option value="Even">Even</option><option value="Mark">Mark</option><option value="Space">Space</option>
            </select>
          </div>
          <div class="control-group">
            <label>停止位</label>
            <select v-model="stopBits" class="morandi-input flex-1">
              <option :value="1">1</option><option :value="15">1.5</option><option :value="2">2</option>
            </select>
          </div>
        </div>

        <button
            @click="toggleConnection"
            class="w-full py-2.5 rounded-lg font-medium text-white transition-all duration-300 transform active:scale-[0.98] shadow-sm flex items-center justify-center space-x-2 bg-[var(--col-primary)] hover:opacity-90">
          <div v-if="!isConnected" class="w-2 h-2 rounded-full bg-white animate-pulse"></div>
          <span>{{ isConnected ? '断开连接' : '打开串口' }}</span>
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

    <div class="flex-1 flex flex-col min-w-0 p-4 gap-4 transition-colors duration-300">

      <div class="flex-1 bg-white/60 rounded-xl shadow-[0_2px_12px_-4px_rgba(0,0,0,0.08)] border border-black/5 flex flex-col overflow-hidden relative backdrop-blur-sm">

        <div class="h-10 px-4 flex items-center justify-between bg-black/[0.02] border-b border-black/5">
          <div class="flex items-center space-x-2">
            <span class="text-xs font-bold text-[var(--col-primary)] tracking-wider">RX MONITOR</span>
            <span class="text-[10px] text-[var(--text-sub)] bg-black/5 px-1.5 py-0.5 rounded-md">{{ rxCount }} Bytes</span>
          </div>

          <button
              @click="clearReceive"
              title="清空接收区"
              class="group flex items-center justify-center w-7 h-7 rounded hover:bg-white hover:shadow-sm text-[var(--text-sub)] hover:text-[var(--col-primary)] transition-all">
            <svg class="w-4 h-4 group-hover:-rotate-12 transition-transform" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M18 6L7.5 16.5"></path>
              <path d="M19.5 4.5L16.5 7.5"></path>
              <path d="M2 22L4.5 19.5"></path>
              <path d="M9.5 12.5C7.5 14.5 6 15 5 16C4 17 3 17 3 17C3 17 3 18 4 19C5 20 5 20 5 20C5 20 6 20 7 19C8 18 8.5 16.5 10.5 14.5L18 7"></path>
            </svg>
          </button>
        </div>

        <textarea
            ref="logWindowRef"
            readonly
            class="flex-1 w-full p-4 font-mono text-sm bg-transparent resize-none outline-none custom-scrollbar leading-relaxed text-[var(--text-main)]"
            :value="receivedData"
        ></textarea>
      </div>

      <div class="h-40 bg-white/60 rounded-xl shadow-[0_2px_12px_-4px_rgba(0,0,0,0.08)] border border-black/5 flex flex-col overflow-hidden backdrop-blur-sm">
        <div class="h-9 px-4 flex items-center justify-between bg-black/[0.02] border-b border-black/5">
          <div class="flex items-center space-x-4">
            <span class="text-xs font-bold text-[var(--text-sub)] tracking-wider">TX EDITOR</span>
            <label class="flex items-center space-x-1 cursor-pointer hover:text-[var(--col-primary)]">
              <input type="checkbox" v-model="sendHex" class="accent-[var(--col-primary)] w-3 h-3">
              <span class="text-[11px] text-[var(--text-sub)]">Hex Send</span>
            </label>
          </div>
        </div>

        <div class="flex-1 flex p-3 gap-3">
                 <textarea
                     v-model="sendInput"
                     class="flex-1 bg-white/50 border border-transparent focus:border-[var(--col-primary)]/30 rounded-lg p-3 font-mono text-sm text-[var(--text-main)] focus:bg-white transition-all outline-none resize-none placeholder-[var(--text-sub)]/50"
                     placeholder="Input data to send..."
                     @keydown.enter.ctrl.prevent="handleSend"
                 ></textarea>

          <div class="flex flex-col gap-2 w-20">
            <button
                @click="handleSend"
                class="flex-1 bg-[var(--col-primary)] hover:opacity-90 text-white rounded-lg shadow-sm transition-all flex flex-col items-center justify-center active:scale-95">
              <span class="text-xs font-bold tracking-widest">SEND</span>
            </button>
            <button @click="sendInput=''" class="h-8 bg-black/5 text-[var(--text-sub)] hover:bg-black/10 rounded-lg text-xs">
              CLR
            </button>
          </div>
        </div>
      </div>

    </div>
  </div>
</template>

<style scoped>
/* 样式细节 */

.control-group {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.control-group label {
  width: 48px;
  text-align: right;
  font-size: 0.75rem;
  color: var(--text-sub);
}

.morandi-input {
  width: 100%;
  background-color: rgba(255, 255, 255, 0.6);
  border: 1px solid rgba(0, 0, 0, 0.1);
  color: var(--text-main);
  padding: 0.25rem 0.5rem;
  font-size: 0.8rem;
  border-radius: 0.375rem;
  outline: none;
  transition: all 0.2s;
}

.morandi-input:focus {
  background-color: #fff;
  border-color: var(--col-primary);
}

/* 滚动条跟随主题颜色 */
.custom-scrollbar::-webkit-scrollbar {
  width: 6px;
  height: 6px;
}
.custom-scrollbar::-webkit-scrollbar-track {
  background: transparent;
}
.custom-scrollbar::-webkit-scrollbar-thumb {
  background: rgba(0,0,0,0.15);
  border-radius: 3px;
}
.custom-scrollbar::-webkit-scrollbar-thumb:hover {
  background: var(--col-primary);
}
</style>
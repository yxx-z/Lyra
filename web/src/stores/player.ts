import { defineStore } from 'pinia'
import { ref, watch } from 'vue'
import type { PlayerTrack } from '../api/client'

export interface ExtendedPlayerTrack extends PlayerTrack {
  coverUrl?: string
}

export type RepeatMode = 'none' | 'one' | 'all'

export const usePlayerStore = defineStore('player', () => {
  // 实例化全局唯一、持久化的 Audio 实例
  const audio = new Audio()

  // 基础响应式状态 (State)
  const currentTrack = ref<ExtendedPlayerTrack | null>(null)
  const queue = ref<ExtendedPlayerTrack[]>([])
  const currentIndex = ref(-1)
  const isPlaying = ref(false)
  const currentTime = ref(0)
  const duration = ref(0)
  const volume = ref(parseFloat(localStorage.getItem('lyra.volume') || '0.8'))
  const isMuted = ref(false)
  const shuffle = ref(false)
  const repeatMode = ref<RepeatMode>('all') // 默认为列表循环 'all'
  const isLoading = ref(false)
  const playbackError = ref<string | null>(null)

  // 初始化音量与静音状态
  audio.volume = volume.value
  audio.muted = isMuted.value

  // 绑定底层 HTML5 Audio 事件监听
  audio.addEventListener('timeupdate', () => {
    currentTime.value = audio.currentTime
  })

  audio.addEventListener('durationchange', () => {
    duration.value = audio.duration || 0
  })

  audio.addEventListener('play', () => {
    isPlaying.value = true
  })

  audio.addEventListener('pause', () => {
    isPlaying.value = false
  })

  audio.addEventListener('ended', () => {
    handleTrackEnded()
  })

  audio.addEventListener('playing', () => {
    isLoading.value = false
  })

  audio.addEventListener('waiting', () => {
    isLoading.value = true
  })

  audio.addEventListener('error', () => {
    isLoading.value = false
    isPlaying.value = false
    playbackError.value = currentTrack.value
      ? `无法播放《${currentTrack.value.title}》`
      : '播放失败'
  })

  // 自动播放监测与切歌逻辑
  function handleTrackEnded() {
    if (repeatMode.value === 'one') {
      audio.currentTime = 0
      audio.play().catch((err) => console.warn('Autoplay blocked: ', err))
    } else {
      next()
    }
  }

  // 核心操作方法 (Actions)
  
  // 1. 播放指定曲目并自动填充/同步队列
  function playTrack(track: ExtendedPlayerTrack, newQueue?: ExtendedPlayerTrack[]) {
    isLoading.value = true
    playbackError.value = null
    if (newQueue && newQueue.length > 0) {
      queue.value = [...newQueue]
      const idx = queue.value.findIndex(t => t.trackId === track.trackId)
      currentIndex.value = idx !== -1 ? idx : 0
    } else {
      // 如果没有提供队列，则将自己作为单曲队列
      const idx = queue.value.findIndex(t => t.trackId === track.trackId)
      if (idx === -1) {
        queue.value.push(track)
        currentIndex.value = queue.value.length - 1
      } else {
        currentIndex.value = idx
      }
    }

    currentTrack.value = track
    audio.src = withPlaybackNonce(track.streamUrl)
    audio.currentTime = 0
    currentTime.value = 0
    
    // 开始播放
    audio.play()
      .then(() => {
        isPlaying.value = true
      })
      .catch((err) => {
        console.warn('Playback standard start blocked by browser autoplay rules: ', err)
        isPlaying.value = false
        isLoading.value = false
      })
  }

  // 2. 播放/暂停切换
  function togglePlay() {
    if (!currentTrack.value) return
    if (isPlaying.value) {
      audio.pause()
    } else {
      audio.play()
        .then(() => {
          isPlaying.value = true
        })
        .catch((err) => {
          console.warn('Play action blocked: ', err)
        })
    }
  }

  // 3. 播放指定索引的歌曲
  function playAtIndex(index: number) {
    if (index >= 0 && index < queue.value.length) {
      isLoading.value = true
      playbackError.value = null
      currentIndex.value = index
      const track = queue.value[index]
      currentTrack.value = track
      audio.src = withPlaybackNonce(track.streamUrl)
      audio.currentTime = 0
      currentTime.value = 0
      audio.play()
        .then(() => {
          isPlaying.value = true
        })
        .catch((err) => {
          console.warn('Switch track autoplay blocked: ', err)
          isPlaying.value = false
          isLoading.value = false
        })
    }
  }

  // 4. 下一首播放：插到当前曲之后（不打断当前）；队列空则直接开播。
  function playNext(item: ExtendedPlayerTrack) {
    if (queue.value.length === 0) {
      playTrack(item, [item])
      return
    }
    queue.value.splice(currentIndex.value + 1, 0, item)
  }

  // 5. 从队列移除某项（不对当前曲提供移除）。保持 currentTrack 指向不变。
  function removeFromQueue(index: number) {
    if (index < 0 || index >= queue.value.length || index === currentIndex.value) return
    queue.value.splice(index, 1)
    if (index < currentIndex.value) currentIndex.value--
  }

  // 6. 队列内拖拽重排：用对象引用重定位 currentIndex，保证正在播的曲不被打断。
  function moveInQueue(from: number, to: number) {
    if (from < 0 || from >= queue.value.length || to < 0 || to >= queue.value.length || from === to) return
    const cur = queue.value[currentIndex.value]
    const [moved] = queue.value.splice(from, 1)
    queue.value.splice(to, 0, moved)
    currentIndex.value = queue.value.indexOf(cur)
  }

  // 7. 下一首
  function next() {
    if (queue.value.length === 0) return

    if (shuffle.value && queue.value.length > 1) {
      // 随机播放模式：从队列中挑一个与当前索引不同的索引
      let nextIdx = currentIndex.value
      while (nextIdx === currentIndex.value) {
        nextIdx = Math.floor(Math.random() * queue.value.length)
      }
      playAtIndex(nextIdx)
    } else {
      // 顺序播放
      if (currentIndex.value + 1 < queue.value.length) {
        playAtIndex(currentIndex.value + 1)
      } else if (repeatMode.value === 'all') {
        // 列表循环，回到第一首
        playAtIndex(0)
      } else {
        // 播放结束
        isPlaying.value = false
      }
    }
  }

  // 5. 上一首
  function prev() {
    if (queue.value.length === 0) return

    // 如果当前播放进度大于 3 秒，切上一首的行为是重新播放当前曲目
    if (audio.currentTime > 3) {
      seek(0)
      return
    }

    if (shuffle.value && queue.value.length > 1) {
      // 随机播放模式的上一首同样是随机挑一首
      let prevIdx = currentIndex.value
      while (prevIdx === currentIndex.value) {
        prevIdx = Math.floor(Math.random() * queue.value.length)
      }
      playAtIndex(prevIdx)
    } else {
      // 顺序切回上一首
      if (currentIndex.value - 1 >= 0) {
        playAtIndex(currentIndex.value - 1)
      } else if (repeatMode.value === 'all') {
        // 回到最后一首
        playAtIndex(queue.value.length - 1)
      } else {
        // 第一首重播
        seek(0)
      }
    }
  }

  // 6. 快进/快退 (进度跳转)
  function seek(seconds: number) {
    if (!currentTrack.value) return
    const safeSeconds = Math.max(0, Math.min(duration.value, seconds))
    audio.currentTime = safeSeconds
    currentTime.value = safeSeconds
  }

  // 7. 设置音量
  function setVolume(vol: number) {
    const safeVol = Math.max(0, Math.min(1, vol))
    volume.value = safeVol
    audio.volume = safeVol
    localStorage.setItem('lyra.volume', safeVol.toString())
    if (isMuted.value && safeVol > 0) {
      toggleMute()
    }
  }

  // 8. 静音切换
  function toggleMute() {
    isMuted.value = !isMuted.value
    audio.muted = isMuted.value
  }

  function clearError() {
    playbackError.value = null
  }

  function withPlaybackNonce(url: string) {
    const separator = url.includes('?') ? '&' : '?'
    return `${url}${separator}_play=${Date.now()}`
  }

  // 监视并在音量变化时强制纠正 Audio 的状态
  watch(volume, (newVol) => {
    audio.volume = newVol
  })

  return {
    currentTrack,
    queue,
    currentIndex,
    isPlaying,
    currentTime,
    duration,
    volume,
    isMuted,
    shuffle,
    repeatMode,
    playTrack,
    togglePlay,
    playAtIndex,
    playNext,
    removeFromQueue,
    moveInQueue,
    next,
    prev,
    seek,
    setVolume,
    toggleMute,
    isLoading,
    playbackError,
    clearError
  }
})

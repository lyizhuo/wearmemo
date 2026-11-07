package dev.wearmemo

import android.app.Application
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import dev.wearmemo.audio.Recorder
import dev.wearmemo.data.Memo
import dev.wearmemo.data.MemoQueue
import dev.wearmemo.data.MemoStatus
import dev.wearmemo.net.RelayApi
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.File

enum class Screen { CAPTURE, RECORDING, UPLOADED, DETAIL, HISTORY, OFFLINE }

class MemoViewModel(app: Application) : AndroidViewModel(app) {

    private val queue = MemoQueue(app)
    private val recorder = Recorder(app)
    private val api = RelayApi(BuildConfig.RELAY_BASE_URL, BuildConfig.DEVICE_TOKEN)

    var screen by mutableStateOf(Screen.CAPTURE)
        private set
    var elapsedMs by mutableStateOf(0L)
        private set
    var memos by mutableStateOf(queue.all())
        private set
    var selected by mutableStateOf<Memo?>(null)
        private set
    var lastUploaded by mutableStateOf<Memo?>(null)
        private set

    private var ticker: Job? = null
    private var startedAt = 0L

    val pendingCount: Int get() = memos.count {
        it.status == MemoStatus.LOCAL || it.status == MemoStatus.FAILED
    }

    fun lastAgoLabel(): String {
        val last = memos.firstOrNull() ?: return "还没有记录"
        val minutes = (System.currentTimeMillis() - last.createdAt) / 60_000
        return when {
            minutes < 1 -> "上次 刚刚"
            minutes < 60 -> "上次 $minutes 分钟前"
            minutes < 60 * 24 -> "上次 ${minutes / 60} 小时前"
            else -> "上次 ${minutes / (60 * 24)} 天前"
        }
    }

    fun startRecording() {
        if (recorder.isRecording) return
        runCatching { recorder.start() }.onFailure { return }
        startedAt = System.currentTimeMillis()
        elapsedMs = 0L
        screen = Screen.RECORDING
        ticker?.cancel()
        ticker = viewModelScope.launch {
            while (true) {
                elapsedMs = System.currentTimeMillis() - startedAt
                delay(200)
            }
        }
    }

    fun cancelRecording() {
        ticker?.cancel()
        ticker = null
        recorder.cancel()
        elapsedMs = 0L
        screen = Screen.CAPTURE
    }

    fun stopRecording() {
        ticker?.cancel()
        ticker = null
        val duration = System.currentTimeMillis() - startedAt
        val file = recorder.stop()
        if (file == null) {
            screen = Screen.CAPTURE
            return
        }

        val memo = Memo(
            createdAt = startedAt,
            durationMs = duration,
            audioPath = file.absolutePath,
            status = MemoStatus.LOCAL,
        )
        queue.upsert(memo)
        refresh()
        lastUploaded = memo
        screen = Screen.UPLOADED
        upload(memo, file)
    }

    private fun upload(memo: Memo, file: File) {
        viewModelScope.launch {
            var current = memo.copy(status = MemoStatus.UPLOADING)
            queue.upsert(current)
            refresh()

            val jobId = try {
                withContext(Dispatchers.IO) { api.upload(file) }
            } catch (t: Throwable) {
                current = current.copy(status = MemoStatus.FAILED, error = t.message)
                queue.upsert(current)
                refresh()
                lastUploaded = current
                screen = Screen.OFFLINE
                return@launch
            }

            current = current.copy(status = MemoStatus.TRANSCRIBING)
            queue.upsert(current)
            refresh()

            // ASR on the ARM host can take a while; poll patiently but bounded.
            repeat(150) {
                delay(2_000)
                val job = runCatching { withContext(Dispatchers.IO) { api.poll(jobId) } }.getOrNull()
                    ?: return@repeat

                if (job.isDone) {
                    current = current.copy(
                        status = MemoStatus.DONE,
                        text = job.text,
                        noteId = job.noteId,
                    )
                    queue.upsert(current)
                    refresh()
                    if (lastUploaded?.id == current.id) lastUploaded = current
                    runCatching { file.delete() }
                    return@launch
                }
                if (job.isFailed) {
                    current = current.copy(status = MemoStatus.FAILED, error = job.error)
                    queue.upsert(current)
                    refresh()
                    if (lastUploaded?.id == current.id) lastUploaded = current
                    return@launch
                }
            }
        }
    }

    fun retryPending() {
        val targets = queue.pending()
        targets.forEach { memo ->
            val path = memo.audioPath ?: return@forEach
            val file = File(path)
            if (!file.exists()) return@forEach
            if (memo.status == MemoStatus.FAILED) upload(memo, file)
        }
        if (targets.isEmpty()) screen = Screen.CAPTURE
    }

    fun retry(memo: Memo) {
        val path = memo.audioPath ?: return
        val file = File(path)
        if (!file.exists()) return
        upload(memo, file)
    }

    fun openDetail(memo: Memo) {
        selected = memo
        screen = Screen.DETAIL
    }

    fun openHistory() {
        screen = Screen.HISTORY
    }

    fun back() {
        screen = when (screen) {
            Screen.DETAIL -> Screen.HISTORY
            Screen.HISTORY, Screen.OFFLINE, Screen.UPLOADED -> Screen.CAPTURE
            else -> Screen.CAPTURE
        }
    }

    fun selectedOrLast(): Memo? = selected ?: memos.firstOrNull()

    private fun refresh() {
        memos = queue.all()
    }
}

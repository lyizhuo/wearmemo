package dev.lyizhuo.wearmemo.audio

import android.content.Context
import android.media.MediaRecorder
import android.os.Build
import java.io.File

/**
 * 16 kHz mono AAC in an MP4 container — small enough to upload over a flaky
 * watch link, and universally decodable by the relay's ffmpeg/ASR path.
 */
class Recorder(private val context: Context) {

    private var recorder: MediaRecorder? = null
    private var current: File? = null

    val isRecording: Boolean get() = recorder != null

    fun start(): File {
        require(recorder == null) { "already recording" }

        val file = File(context.cacheDir, "memo-${System.currentTimeMillis()}.m4a")
        val r = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            MediaRecorder(context)
        } else {
            @Suppress("DEPRECATION")
            MediaRecorder()
        }

        r.apply {
            setAudioSource(MediaRecorder.AudioSource.MIC)
            setOutputFormat(MediaRecorder.OutputFormat.MPEG_4)
            setAudioEncoder(MediaRecorder.AudioEncoder.AAC)
            setAudioEncodingBitRate(32_000)
            setAudioSamplingRate(16_000)
            setAudioChannels(1)
            setOutputFile(file.absolutePath)
            prepare()
            start()
        }

        recorder = r
        current = file
        return file
    }

    /** @return the finished file, or null when nothing usable was captured. */
    fun stop(): File? {
        val r = recorder ?: return null
        val file = current
        recorder = null
        current = null

        return try {
            r.stop()
            r.release()
            file?.takeIf { it.exists() && it.length() > 512 }
        } catch (t: Throwable) {
            // stop() throws when the recording was too short to write a track.
            runCatching { r.release() }
            file?.delete()
            null
        }
    }

    fun cancel() {
        val r = recorder ?: return
        recorder = null
        runCatching { r.stop() }
        runCatching { r.release() }
        current?.delete()
        current = null
    }
}

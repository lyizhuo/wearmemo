package dev.lyizhuo.wearmemo.audio

import android.annotation.SuppressLint
import android.content.Context
import android.media.AudioFormat
import android.media.AudioRecord
import android.media.MediaRecorder
import java.io.BufferedOutputStream
import java.io.File
import java.io.FileOutputStream
import java.io.RandomAccessFile
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * Records 16 kHz mono PCM16 WAV.
 *
 * WAV rather than AAC on purpose: the ASR host takes the audio base64-encoded
 * straight into Ollama's `images` field and expects WAV, so recording in the
 * final format keeps the relay free of an ffmpeg dependency. 60 s costs ~1.9 MB,
 * which is nothing next to a failed upload.
 */
class Recorder(private val context: Context) {

    private var record: AudioRecord? = null
    private var worker: Thread? = null
    private var target: File? = null

    @Volatile
    private var running = false

    @Volatile
    private var dataBytes = 0

    val isRecording: Boolean get() = running

    @SuppressLint("MissingPermission")
    fun start(): File {
        check(!running) { "already recording" }

        val file = File(context.cacheDir, "memo-${System.currentTimeMillis()}.wav")
        val minBuf = AudioRecord.getMinBufferSize(SAMPLE_RATE, CHANNEL, ENCODING)
        val bufSize = maxOf(minBuf, SAMPLE_RATE)

        val rec = AudioRecord(MediaRecorder.AudioSource.MIC, SAMPLE_RATE, CHANNEL, ENCODING, bufSize)
        if (rec.state != AudioRecord.STATE_INITIALIZED) {
            rec.release()
            error("AudioRecord failed to initialise")
        }
        rec.startRecording()

        val out = BufferedOutputStream(FileOutputStream(file))
        out.write(ByteArray(HEADER_BYTES))

        dataBytes = 0
        running = true
        record = rec
        target = file

        worker = Thread {
            val buf = ByteArray(bufSize)
            try {
                while (running) {
                    val n = rec.read(buf, 0, buf.size)
                    if (n > 0) {
                        out.write(buf, 0, n)
                    }
                }
            } catch (_: Throwable) {
                // device lost the mic; the caller decides what to do with a short file
            } finally {
                runCatching { out.flush() }
                runCatching { out.close() }
            }
        }.apply { start() }

        return file
    }

    /** @return the finished file, or null when nothing usable was captured. */
    fun stop(): File? {
        if (!running) return null
        running = false

        record?.let {
            runCatching { it.stop() }
            runCatching { it.release() }
        }
        record = null
        worker?.join(1_500)
        worker = null

        val file = target ?: return null
        target = null

        val samples = file.length() - HEADER_BYTES
        if (samples <= 0) {
            file.delete()
            return null
        }
        runCatching { patchHeader(file, samples.toInt()) }

        // Under ~120 ms of audio is a mis-tap, not a memo.
        return file.takeIf { it.exists() && it.length() > HEADER_BYTES + SAMPLE_RATE / 8 }
    }

    fun cancel() {
        running = false
        record?.let {
            runCatching { it.stop() }
            runCatching { it.release() }
        }
        record = null
        worker?.join(800)
        worker = null
        target?.delete()
        target = null
    }

    private fun patchHeader(file: File, dataLen: Int) {
        RandomAccessFile(file, "rw").use { raf ->
            raf.seek(0)
            raf.write(buildHeader(dataLen))
        }
    }

    companion object {
        private const val SAMPLE_RATE = 16_000
        private const val CHANNEL = AudioFormat.CHANNEL_IN_MONO
        private const val ENCODING = AudioFormat.ENCODING_PCM_16BIT
        private const val HEADER_BYTES = 44

        fun buildHeader(dataLen: Int): ByteArray {
            val b = ByteBuffer.allocate(HEADER_BYTES).order(ByteOrder.LITTLE_ENDIAN)
            b.put("RIFF".toByteArray())
            b.putInt(36 + dataLen)
            b.put("WAVE".toByteArray())
            b.put("fmt ".toByteArray())
            b.putInt(16)            // PCM chunk size
            b.putShort(1.toShort()) // format = PCM
            b.putShort(1.toShort()) // channels
            b.putInt(SAMPLE_RATE)
            b.putInt(SAMPLE_RATE * 2) // byte rate
            b.putShort(2.toShort())   // block align
            b.putShort(16.toShort())  // bits per sample
            b.put("data".toByteArray())
            b.putInt(dataLen)
            return b.array()
        }
    }
}

package dev.wearmemo.data

import android.content.Context
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.util.UUID

enum class MemoStatus {
    /** Recorded, still on the watch. */
    LOCAL,
    /** Bytes are on their way to the relay. */
    UPLOADING,
    /** Relay accepted; ASR + Blinko write are in flight. */
    TRANSCRIBING,
    /** Written to Blinko. */
    DONE,
    /** Terminal failure; audio kept so it can be retried. */
    FAILED,
}

data class Memo(
    val id: String = UUID.randomUUID().toString(),
    val createdAt: Long = System.currentTimeMillis(),
    val durationMs: Long = 0L,
    val audioPath: String? = null,
    val text: String? = null,
    val status: MemoStatus = MemoStatus.LOCAL,
    val noteId: Long? = null,
    val error: String? = null,
) {
    fun toJson(): JSONObject = JSONObject().apply {
        put("id", id)
        put("createdAt", createdAt)
        put("durationMs", durationMs)
        put("audioPath", audioPath)
        put("text", text)
        put("status", status.name)
        put("noteId", noteId)
        put("error", error)
    }

    companion object {
        fun fromJson(o: JSONObject) = Memo(
            id = o.getString("id"),
            createdAt = o.optLong("createdAt"),
            durationMs = o.optLong("durationMs"),
            audioPath = o.optString("audioPath").ifEmpty { null },
            text = o.optString("text").ifEmpty { null },
            status = runCatching { MemoStatus.valueOf(o.optString("status")) }
                .getOrDefault(MemoStatus.LOCAL),
            noteId = if (o.isNull("noteId")) null else o.optLong("noteId"),
            error = o.optString("error").ifEmpty { null },
        )
    }
}

/**
 * Durable local queue. Deliberately file-backed JSON rather than Room:
 * the whole dataset is a handful of pending memos, and this keeps the
 * first launch instant with zero migration surface.
 */
class MemoQueue(context: Context) {

    private val file = File(context.filesDir, "queue.json")
    private val lock = Any()
    private var cache: MutableList<Memo>? = null

    fun all(): List<Memo> = synchronized(lock) {
        val list = cache ?: read().also { cache = it }
        list.sortedByDescending { it.createdAt }
    }

    fun upsert(memo: Memo) = synchronized(lock) {
        val list = cache ?: read().also { cache = it }
        val i = list.indexOfFirst { it.id == memo.id }
        if (i >= 0) list[i] = memo else list.add(memo)
        write(list)
    }

    fun find(id: String): Memo? = all().firstOrNull { it.id == id }

    fun pending(): List<Memo> = all().filter {
        it.status == MemoStatus.LOCAL || it.status == MemoStatus.FAILED
    }

    private fun read(): MutableList<Memo> {
        if (!file.exists()) return mutableListOf()
        return runCatching {
            val arr = JSONArray(file.readText())
            MutableList(arr.length()) { Memo.fromJson(arr.getJSONObject(it)) }
        }.getOrElse { mutableListOf() }
    }

    private fun write(list: List<Memo>) {
        val arr = JSONArray()
        list.forEach { arr.put(it.toJson()) }
        val tmp = File(file.parentFile, "queue.json.tmp")
        tmp.writeText(arr.toString())
        if (!tmp.renameTo(file)) {
            file.writeText(arr.toString())
            tmp.delete()
        }
    }
}

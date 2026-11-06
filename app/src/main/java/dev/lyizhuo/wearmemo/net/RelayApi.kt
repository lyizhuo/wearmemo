package dev.lyizhuo.wearmemo.net

import org.json.JSONObject
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.util.UUID

/**
 * Talks to our relay, never to ASR or Blinko directly: the relay is the only
 * place that holds the Blinko apiToken and the ASR credentials.
 */
class RelayApi(
    private val baseUrl: String,
    private val deviceToken: String,
) {

    data class Job(
        val id: String,
        val status: String,
        val text: String?,
        val noteId: Long?,
        val error: String?,
    ) {
        val isDone: Boolean get() = status == "done"
        val isFailed: Boolean get() = status == "failed"
    }

    /** POST /v1/memos -> 202 { id }. Returns the relay job id. */
    fun upload(file: File): String {
        val boundary = "----wm" + UUID.randomUUID().toString().replace("-", "")
        val conn = (URL("$baseUrl/v1/memos").openConnection() as HttpURLConnection).apply {
            requestMethod = "POST"
            doOutput = true
            connectTimeout = 15_000
            readTimeout = 120_000
            setRequestProperty("Authorization", "Bearer $deviceToken")
            setRequestProperty("Content-Type", "multipart/form-data; boundary=$boundary")
        }

        try {
            conn.outputStream.use { out ->
                fun field(name: String, value: String) {
                    out.write("--$boundary\r\n".toByteArray())
                    out.write("Content-Disposition: form-data; name=\"$name\"\r\n\r\n".toByteArray())
                    out.write(value.toByteArray())
                    out.write("\r\n".toByteArray())
                }

                field("clientId", file.nameWithoutExtension)
                field("clientEpochMs", file.lastModified().toString())

                out.write("--$boundary\r\n".toByteArray())
                out.write(
                    "Content-Disposition: form-data; name=\"audio\"; filename=\"${file.name}\"\r\n"
                        .toByteArray()
                )
                out.write("Content-Type: audio/mp4\r\n\r\n".toByteArray())
                file.inputStream().use { it.copyTo(out) }
                out.write("\r\n--$boundary--\r\n".toByteArray())
            }

            val code = conn.responseCode
            val body = (if (code in 200..299) conn.inputStream else conn.errorStream)
                ?.bufferedReader()?.use { it.readText() } ?: ""
            if (code !in 200..299) error("relay $code: ${body.take(200)}")

            return JSONObject(body).optString("id").ifEmpty {
                error("relay returned no id: ${body.take(200)}")
            }
        } finally {
            conn.disconnect()
        }
    }

    /** GET /v1/memos/{id}. Polled while the watch shows "正在转写". */
    fun poll(jobId: String): Job {
        val conn = (URL("$baseUrl/v1/memos/$jobId").openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            connectTimeout = 15_000
            readTimeout = 20_000
            setRequestProperty("Authorization", "Bearer $deviceToken")
        }

        try {
            val code = conn.responseCode
            val body = (if (code in 200..299) conn.inputStream else conn.errorStream)
                ?.bufferedReader()?.use { it.readText() } ?: ""
            if (code !in 200..299) error("relay $code: ${body.take(200)}")

            val o = JSONObject(body)
            return Job(
                id = jobId,
                status = o.optString("status", "queued"),
                text = o.optString("text").ifEmpty { null },
                noteId = if (o.has("noteId") && !o.isNull("noteId")) o.optLong("noteId") else null,
                error = o.optString("error").ifEmpty { null },
            )
        } finally {
            conn.disconnect()
        }
    }
}

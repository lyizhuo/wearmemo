package dev.wearmemo.ui

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.ColorFilter
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.foundation.gestures.detectVerticalDragGestures
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.wearmemo.R
import dev.wearmemo.data.Memo
import dev.wearmemo.data.MemoStatus

/* ------------------------------------------------------------------ */
/*  Primitives                                                        */
/* ------------------------------------------------------------------ */

@Composable
private fun CircleButton(
    size: Int,
    background: Color,
    icon: Int?,
    iconTint: Color = Color.White,
    iconScale: Float = 1.6f,
    onClick: () -> Unit,
) {
    Box(
        modifier = Modifier
            .size(size.dp)
            .clip(CircleShape)
            .background(background)
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        if (icon != null) {
            Image(
                painter = painterResource(icon),
                contentDescription = null,
                colorFilter = ColorFilter.tint(iconTint),
                modifier = Modifier.size((24 * iconScale).dp),
            )
        }
    }
}

private fun fmt(ms: Long): String {
    val total = ms / 1000
    return "%02d:%02d".format(total / 60, total % 60)
}

/* ------------------------------------------------------------------ */
/*  01 · Capture                                                      */
/* ------------------------------------------------------------------ */

@Composable
fun CaptureScreen(lastAgo: String, onStart: () -> Unit, onHistory: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(WM.Background),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        CircleButton(96, WM.Record, R.drawable.ic_mic, iconScale = 1.7f, onClick = onStart)
        Spacer(Modifier.height(18.dp))
        Text(
            "轻点说话",
            fontSize = 15.sp,
            fontWeight = FontWeight.Medium,
            color = WM.Label,
        )
        Spacer(Modifier.height(6.dp))
        Text(
            lastAgo,
            fontSize = 12.sp,
            color = WM.Label2,
            modifier = Modifier.clickable(onClick = onHistory),
        )
    }
}

/* ------------------------------------------------------------------ */
/*  02 · Recording                                                    */
/* ------------------------------------------------------------------ */

@Composable
fun RecordingScreen(elapsedMs: Long, onStop: () -> Unit, onCancel: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(WM.Background)
            .pointerInput(Unit) {
                var acc = 0f
                detectVerticalDragGestures(
                    onDragEnd = {
                        if (acc < -80f) onCancel()
                        acc = 0f
                    },
                    onDragCancel = { acc = 0f },
                    onVerticalDrag = { _, dragAmount -> acc += dragAmount },
                )
            },
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.size(6.dp).clip(CircleShape).background(WM.Record))
            Spacer(Modifier.width(11.dp))
            Text("录音中", fontSize = 12.sp, fontWeight = FontWeight.Medium, color = WM.Record)
        }
        Spacer(Modifier.height(20.dp))
        Text(
            fmt(elapsedMs),
            fontSize = 38.sp,
            color = WM.Label,
        )
        Spacer(Modifier.height(10.dp))
        Waveform()
        Spacer(Modifier.height(22.dp))
        StopButton(onStop)
        Spacer(Modifier.height(6.dp))
        Text("上滑取消", fontSize = 11.sp, color = WM.Label3)
    }
}

@Composable
private fun StopButton(onClick: () -> Unit) {
    Box(
        modifier = Modifier
            .size(64.dp)
            .clip(CircleShape)
            .background(WM.Record)
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Box(
            Modifier
                .size(20.dp)
                .clip(RoundedCornerShape(5.dp))
                .background(Color.White)
        )
    }
}

@Composable
private fun Waveform() {
    val heights = listOf(6, 12, 18, 10, 16, 8, 5)
    Row(
        horizontalArrangement = Arrangement.spacedBy(6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        heights.forEach { h ->
            Box(
                Modifier
                    .width(3.dp)
                    .height(h.dp)
                    .clip(RoundedCornerShape(1.5.dp))
                    .background(WM.Record)
            )
        }
    }
}

/* ------------------------------------------------------------------ */
/*  03 · Uploaded                                                     */
/* ------------------------------------------------------------------ */

@Composable
fun UploadedScreen(memo: Memo?, onOpen: () -> Unit) {
    val failed = memo?.status == MemoStatus.FAILED
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(WM.Background),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Box(
            modifier = Modifier
                .size(60.dp)
                .clip(CircleShape)
                .background(if (failed) WM.Pending else WM.Success),
            contentAlignment = Alignment.Center,
        ) {
            Image(
                painter = painterResource(if (failed) R.drawable.ic_retry else R.drawable.ic_check),
                contentDescription = null,
                colorFilter = ColorFilter.tint(Color.White),
                modifier = Modifier.size(32.dp),
            )
        }
        Spacer(Modifier.height(18.dp))
        Text(
            if (failed) "没听清" else "已上传",
            fontSize = 20.sp,
            fontWeight = FontWeight.SemiBold,
            color = WM.Label,
        )
        Spacer(Modifier.height(6.dp))
        Text(
            when (memo?.status) {
                MemoStatus.DONE -> "已写入 Blinko"
                MemoStatus.FAILED -> "没有识别到语音"
                else -> "正在转写"
            },
            fontSize = 13.sp,
            color = WM.Label2,
        )
        Spacer(Modifier.height(5.dp))
        Text(
            if (failed) "音频已保留，可在历史里重试" else "完成后通知你，可先息屏",
            fontSize = 11.sp,
            color = WM.Label3,
            textAlign = TextAlign.Center,
        )
    }
}

/* ------------------------------------------------------------------ */
/*  05 · Detail                                                       */
/* ------------------------------------------------------------------ */

@Composable
fun DetailScreen(memo: Memo?, onBack: () -> Unit, onRetry: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(WM.Background)
            .padding(horizontal = 26.dp),
        verticalArrangement = Arrangement.Center,
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text("刚刚", fontSize = 11.sp, color = WM.Label3)
            Spacer(Modifier.weight(1f))
            val (label, color) = when (memo?.status) {
                MemoStatus.DONE -> "已入库" to WM.Success
                MemoStatus.FAILED -> "待重试" to WM.Pending
                MemoStatus.TRANSCRIBING -> "转写中" to WM.Label2
                else -> "待上传" to WM.Pending
            }
            Text(label, fontSize = 11.sp, fontWeight = FontWeight.Medium, color = color)
        }

        Spacer(Modifier.height(10.dp))

        Text(
            memo?.text ?: "还没有转写结果",
            fontSize = 13.sp,
            lineHeight = 20.sp,
            color = if (memo?.text == null) WM.Label2 else WM.Label,
            maxLines = 6,
            overflow = TextOverflow.Ellipsis,
        )

        Spacer(Modifier.height(10.dp))
        Text("#工作", fontSize = 11.sp, color = WM.Label2)
        Spacer(Modifier.height(14.dp))

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.Center,
        ) {
            CircleButton(44, WM.Surface, R.drawable.ic_copy, WM.Label, 0.75f) { }
            Spacer(Modifier.width(22.dp))
            CircleButton(44, WM.Surface, R.drawable.ic_retry, WM.Label, 0.75f, onRetry)
            Spacer(Modifier.width(22.dp))
            CircleButton(44, WM.Surface, R.drawable.ic_phone, WM.Label, 0.75f, onBack)
        }
    }
}

/* ------------------------------------------------------------------ */
/*  06 · History                                                      */
/* ------------------------------------------------------------------ */

@Composable
fun HistoryScreen(memos: List<Memo>, onOpen: (Memo) -> Unit, onBack: () -> Unit) {
    val shown = memos.take(4)
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(WM.Background)
            .padding(horizontal = 22.dp),
        verticalArrangement = Arrangement.Center,
    ) {
        Text(
            "最近 ${shown.size} 条",
            fontSize = 11.sp,
            color = WM.Label3,
            textAlign = TextAlign.Center,
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(10.dp))
        shown.forEach { memo ->
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clickable { onOpen(memo) }
                    .padding(vertical = 5.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    relTime(memo.createdAt),
                    fontSize = 10.sp,
                    color = WM.Label3,
                    modifier = Modifier.width(38.dp),
                )
                Text(
                    memo.text ?: "转写中…",
                    fontSize = 13.sp,
                    color = if (memo.text == null) WM.Label2 else WM.Label,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                Spacer(Modifier.width(8.dp))
                Box(
                    Modifier.size(4.dp).clip(CircleShape).background(
                        when (memo.status) {
                            MemoStatus.DONE -> WM.Label2
                            MemoStatus.FAILED, MemoStatus.LOCAL -> WM.Pending
                            else -> WM.Label3
                        }
                    )
                )
            }
        }
        Spacer(Modifier.height(12.dp))
        Text(
            "在 Blinko 看全部",
            fontSize = 10.sp,
            color = WM.Label3,
            textAlign = TextAlign.Center,
            modifier = Modifier.fillMaxWidth(),
        )
    }
}

private fun relTime(ts: Long): String {
    val minutes = (System.currentTimeMillis() - ts) / 60_000
    return when {
        minutes < 1 -> "刚刚"
        minutes < 60 -> "%02d:%02d".format(
            (ts / 3_600_000L % 24).toInt(),
            (ts / 60_000L % 60).toInt(),
        )
        minutes < 60 * 24 -> "昨天"
        else -> "${minutes / (60 * 24)} 天前"
    }
}

/* ------------------------------------------------------------------ */
/*  07 · Offline                                                      */
/* ------------------------------------------------------------------ */

@Composable
fun OfflineScreen(count: Int, onRetry: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(WM.Background),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Image(
            painter = painterResource(R.drawable.ic_cloud_off),
            contentDescription = null,
            colorFilter = ColorFilter.tint(WM.Pending),
            modifier = Modifier.size(40.dp),
        )
        Spacer(Modifier.height(16.dp))
        Text("$count 条待上传", fontSize = 17.sp, fontWeight = FontWeight.SemiBold, color = WM.Label)
        Spacer(Modifier.height(6.dp))
        Text("网络已断开，联网后自动重试", fontSize = 11.sp, color = WM.Label2)
        Spacer(Modifier.height(18.dp))
        Box(
            modifier = Modifier
                .height(34.dp)
                .width(92.dp)
                .clip(RoundedCornerShape(17.dp))
                .background(WM.SurfaceRaised)
                .clickable(onClick = onRetry),
            contentAlignment = Alignment.Center,
        ) {
            Text("重试", fontSize = 13.sp, fontWeight = FontWeight.Medium, color = WM.Label)
        }
    }
}

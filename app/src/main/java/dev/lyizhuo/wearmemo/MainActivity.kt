package dev.lyizhuo.wearmemo

import android.Manifest
import android.content.pm.PackageManager
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.ContextCompat
import androidx.lifecycle.viewmodel.compose.viewModel
import dev.lyizhuo.wearmemo.ui.CaptureScreen
import dev.lyizhuo.wearmemo.ui.DetailScreen
import dev.lyizhuo.wearmemo.ui.HistoryScreen
import dev.lyizhuo.wearmemo.ui.OfflineScreen
import dev.lyizhuo.wearmemo.ui.RecordingScreen
import dev.lyizhuo.wearmemo.ui.UploadedScreen
import dev.lyizhuo.wearmemo.ui.WM

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent { WearMemoApp() }
    }
}

@Composable
fun WearMemoApp(vm: MemoViewModel = viewModel()) {
    val context = LocalContext.current
    val permission = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { granted -> if (granted) vm.startRecording() }

    Surface(color = WM.Background, modifier = Modifier.fillMaxSize()) {
        when (vm.screen) {
            Screen.CAPTURE -> CaptureScreen(
                lastAgo = vm.lastAgoLabel(),
                onStart = {
                    val granted = ContextCompat.checkSelfPermission(
                        context, Manifest.permission.RECORD_AUDIO
                    ) == PackageManager.PERMISSION_GRANTED
                    if (granted) vm.startRecording()
                    else permission.launch(Manifest.permission.RECORD_AUDIO)
                },
                onHistory = vm::openHistory,
            )

            Screen.RECORDING -> RecordingScreen(
                elapsedMs = vm.elapsedMs,
                onStop = vm::stopRecording,
                onCancel = vm::cancelRecording,
            )

            Screen.UPLOADED -> UploadedScreen(
                memo = vm.lastUploaded,
                onOpen = vm::back,
            )

            Screen.DETAIL -> {
                val memo = vm.selectedOrLast()
                DetailScreen(
                    memo = memo,
                    onBack = vm::back,
                    onRetry = { memo?.let(vm::retry) },
                )
            }

            Screen.HISTORY -> HistoryScreen(
                memos = vm.memos,
                onOpen = vm::openDetail,
                onBack = vm::back,
            )

            Screen.OFFLINE -> OfflineScreen(
                count = vm.pendingCount,
                onRetry = vm::retryPending,
            )
        }
    }
}

package dev.lyizhuo.wearmemo.ui

import androidx.compose.ui.graphics.Color

/** Single source of truth for the prototype's colour roles. */
object WM {
    val Background = Color(0xFF000000)
    val Surface = Color(0xFF1C1C1E)
    val SurfaceRaised = Color(0xFF2C2C2E)

    val Label = Color(0xFFFFFFFF)
    val Label2 = Color(0xFF9A9AA0)
    val Label3 = Color(0xFF7C7C82)

    /** Reserved for recording state only. */
    val Record = Color(0xFFFF453A)
    /** Reserved for "written to Blinko". */
    val Success = Color(0xFF30D158)
    /** Reserved for "queued / will retry". */
    val Pending = Color(0xFFFF9F0A)
}

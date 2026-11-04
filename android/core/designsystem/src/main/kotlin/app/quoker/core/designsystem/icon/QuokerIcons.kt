package app.quoker.core.designsystem.icon

import androidx.annotation.DrawableRes
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalContentColor
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import app.quoker.core.designsystem.R

/** Line icons (24dp viewport, round caps) as drawable resources; tint them with [QuokerIcon]. */
object QuokerIcons {
    @DrawableRes val Settings = R.drawable.ic_settings
    @DrawableRes val Back = R.drawable.ic_back
    @DrawableRes val ChevronRight = R.drawable.ic_chevron_right
    @DrawableRes val ChevronDown = R.drawable.ic_chevron_down
    @DrawableRes val Close = R.drawable.ic_close
    @DrawableRes val Paste = R.drawable.ic_paste
    @DrawableRes val Download = R.drawable.ic_download
    @DrawableRes val Send = R.drawable.ic_send
    @DrawableRes val Check = R.drawable.ic_check
    @DrawableRes val Error = R.drawable.ic_error
    @DrawableRes val Warning = R.drawable.ic_warning
    @DrawableRes val Refresh = R.drawable.ic_refresh
    @DrawableRes val OpenExternal = R.drawable.ic_open_external
    @DrawableRes val Copy = R.drawable.ic_copy
    @DrawableRes val Share = R.drawable.ic_share
    @DrawableRes val User = R.drawable.ic_user
    @DrawableRes val Link = R.drawable.ic_link
    @DrawableRes val Clock = R.drawable.ic_clock
    @DrawableRes val Shield = R.drawable.ic_shield
    @DrawableRes val Bookmark = R.drawable.ic_bookmark
    @DrawableRes val ProgressArc = R.drawable.ic_progress_arc
    @DrawableRes val Play = R.drawable.ic_play

    /** White monochrome frog for notifications (`setSmallIcon`). */
    @DrawableRes val Notification = R.drawable.ic_notification
}

@Composable
fun QuokerIcon(
    @DrawableRes icon: Int,
    contentDescription: String?,
    modifier: Modifier = Modifier,
    tint: Color = LocalContentColor.current,
    size: Dp = 24.dp,
) {
    Icon(
        painter = painterResource(icon),
        contentDescription = contentDescription,
        modifier = modifier.size(size),
        tint = tint,
    )
}

/** The lime frog. [inverse] is the dark frog for lime backgrounds. */
@Composable
fun QuokerLogo(
    modifier: Modifier = Modifier,
    size: Dp = 32.dp,
    inverse: Boolean = false,
    contentDescription: String? = null,
) {
    Image(
        painter = painterResource(if (inverse) R.drawable.ic_logo_inverse else R.drawable.ic_logo),
        contentDescription = contentDescription,
        modifier = modifier.size(size),
    )
}

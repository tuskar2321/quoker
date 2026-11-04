package app.quoker.core.designsystem.component

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import app.quoker.core.designsystem.icon.QuokerIcon
import app.quoker.core.designsystem.icon.QuokerIcons
import app.quoker.core.designsystem.theme.QuokerTheme
import coil3.compose.AsyncImage

/** Cover sizes: Sheet 112×198/14, History 64×88/12, Recent 48×64/10. */
enum class CoverSize { Sheet, History, Recent }

/**
 * Video cover loaded with Coil ([model]: URL or ImageRequest; the app's ImageLoader adds auth).
 * Shows the `placeholder` color while loading/without a model. [duration] and [showPlay] are used
 * in the sheet.
 */
@Composable
fun VideoCover(
    model: Any?,
    size: CoverSize,
    modifier: Modifier = Modifier,
    duration: String? = null,
    showPlay: Boolean = false,
    contentDescription: String? = null,
) {
    val d = QuokerTheme.dimens
    val (dpSize, radius) = when (size) {
        CoverSize.Sheet -> d.coverSheet to d.coverSheetRadius
        CoverSize.History -> d.coverHistory to d.coverHistoryRadius
        CoverSize.Recent -> d.coverRecent to d.coverRecentRadius
    }
    val shape = RoundedCornerShape(radius)
    Box(
        modifier = modifier.size(dpSize).clip(shape).background(QuokerTheme.colors.placeholder, shape),
        contentAlignment = Alignment.Center,
    ) {
        if (model != null) {
            AsyncImage(
                model = model,
                contentDescription = contentDescription,
                contentScale = ContentScale.Crop,
                modifier = Modifier.fillMaxSize(),
            )
        }
        if (showPlay) {
            QuokerIcon(QuokerIcons.Play, contentDescription = null, tint = QuokerTheme.colors.onSurface, size = 32.dp)
        }
        if (duration != null) {
            DurationBadge(duration, Modifier.align(Alignment.BottomEnd).padding(8.dp))
        }
    }
}

@Preview
@Composable
private fun VideoCoverPreview() {
    QuokerTheme {
        Row(
            modifier = Modifier.background(QuokerTheme.colors.surface).padding(20.dp),
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            VideoCover(null, CoverSize.Sheet, duration = "0:27", showPlay = true)
            VideoCover(null, CoverSize.History)
            VideoCover(null, CoverSize.Recent)
        }
    }
}

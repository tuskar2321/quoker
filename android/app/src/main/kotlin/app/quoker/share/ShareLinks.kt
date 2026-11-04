package app.quoker.share

import android.content.Intent
import app.quoker.core.common.link.LinkExtractor
import app.quoker.feature.video.VideoSource

/** What a shared ACTION_SEND text opens in the "Видео" sheet. */
object ShareLinks {
    fun source(intent: Intent): VideoSource = source(
        text = intent.getCharSequenceExtra(Intent.EXTRA_TEXT)?.toString(),
        subject = intent.getCharSequenceExtra(Intent.EXTRA_SUBJECT)?.toString(),
    )

    /**
     * The first supported link of EXTRA_TEXT, then of EXTRA_SUBJECT. Without one the raw text goes
     * to the sheet anyway, which shows the matching unsupported_url / unsupported_content state.
     */
    fun source(text: String?, subject: String?): VideoSource {
        val link = listOfNotNull(text, subject).firstNotNullOfOrNull(LinkExtractor::firstSupported)
        if (link != null) return VideoSource.Url(link.url)
        return VideoSource.Url(text?.takeIf { it.isNotBlank() } ?: subject.orEmpty())
    }
}

package app.quoker.share

import android.content.Intent
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.quoker.feature.video.VideoSource
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

@RunWith(AndroidJUnit4::class)
@Config(application = android.app.Application::class)
class ShareLinksTest {
    private data class Case(val name: String, val text: String?, val subject: String?, val expected: VideoSource)

    private val tiktok = "https://www.tiktok.com/@user.name_1/video/7301234567890123456"
    private val instagram = "https://www.instagram.com/reel/C1a2B3c4D5e/"

    private val cases = listOf(
        Case(
            name = "link in share text, query stripped",
            text = "Смотри! $tiktok?is_from_webapp=1&sender_device=pc #fyp",
            subject = null,
            expected = VideoSource.Url(tiktok),
        ),
        Case(
            name = "text wins over subject",
            text = tiktok,
            subject = instagram,
            expected = VideoSource.Url(tiktok),
        ),
        Case(
            name = "subject when text has no link",
            text = "Посмотри это видео",
            subject = instagram,
            expected = VideoSource.Url(instagram),
        ),
        Case(
            name = "subject only",
            text = null,
            subject = "Reel: $instagram",
            expected = VideoSource.Url(instagram),
        ),
        Case(
            name = "unsupported text goes to the sheet as is",
            text = "https://youtube.com/watch?v=abc",
            subject = null,
            expected = VideoSource.Url("https://youtube.com/watch?v=abc"),
        ),
        Case(
            name = "blank text falls back to subject",
            text = "  ",
            subject = "просто текст",
            expected = VideoSource.Url("просто текст"),
        ),
        Case(name = "nothing shared", text = null, subject = null, expected = VideoSource.Url("")),
    )

    @Test
    fun extractsFirstSupportedLinkFromIntentExtras() {
        for (case in cases) {
            val intent = Intent(Intent.ACTION_SEND).setType("text/plain").apply {
                case.text?.let { putExtra(Intent.EXTRA_TEXT, it) }
                case.subject?.let { putExtra(Intent.EXTRA_SUBJECT, it) }
            }
            assertEquals(case.name, case.expected, ShareLinks.source(intent))
        }
    }
}

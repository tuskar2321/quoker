package app.quoker.core.designsystem.text

import android.content.res.Resources
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.quoker.core.common.QuokerError
import app.quoker.core.common.format.ByteSize
import app.quoker.core.common.format.DayBucket
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Test
import org.junit.runner.RunWith
import java.time.LocalDate

@RunWith(AndroidJUnit4::class)
class TextsTest {
    private val res: Resources = ApplicationProvider.getApplicationContext<android.content.Context>().resources

    private val apiCodes = listOf(
        "unauthorized", "forbidden", "user_blocked", "upgrade_required", "invalid_request", "invalid_invite",
        "telegram_not_linked", "link_token_expired", "unsupported_url", "unsupported_content", "private_video",
        "not_found", "file_not_ready", "too_large", "target_inactive", "target_forbidden", "rate_limited",
        "worker_unavailable", "upstream_error", "internal",
    )

    @Test
    fun everyApiCodeHasAString() {
        for (code in apiCodes) assertNotNull(code, errorStringRes(code))
    }

    @Test
    fun userMessage() {
        val cases = listOf(
            QuokerError("invalid_invite", "server text", false) to "Код не подошёл",
            QuokerError("too_large", "", false) to "Видео больше 50 МБ — в Telegram не отправить, но можно скачать",
            QuokerError("brand_new_code", "Текст сервера", true) to "Текст сервера",
            QuokerError("brand_new_code", "  ", true) to "Что-то пошло не так. Попробуйте ещё раз",
        )
        for ((error, want) in cases) assertEquals(error.code, want, error.userMessage(res))
    }

    @Test
    fun sizesAndDays() {
        assertEquals("14 МБ", ByteSize.of(14_680_064).text(res))
        assertEquals("1,2 ГБ", ByteSize.of(1_288_490_189).text(res))
        assertEquals("Сегодня", DayBucket.Today.label(res))
        assertEquals("Вчера", DayBucket.Yesterday.label(res))
        assertEquals("9 ноября", DayBucket.Date(LocalDate.of(2026, 11, 9), sameYear = true).label(res))
        assertEquals("31 декабря 2025", DayBucket.Date(LocalDate.of(2025, 12, 31), sameYear = false).label(res))
    }
}

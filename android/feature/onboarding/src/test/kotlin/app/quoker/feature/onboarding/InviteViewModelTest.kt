package app.quoker.feature.onboarding

import app.cash.turbine.test
import app.quoker.feature.onboarding.invite.DeviceNameProvider
import app.quoker.feature.onboarding.invite.InviteEvent
import app.quoker.feature.onboarding.invite.InviteViewModel
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

class InviteViewModelTest {
    @get:Rule
    val main = MainDispatcherRule()

    private val auth = FakeAuthRepository()
    private val vm = InviteViewModel(auth, DeviceNameProvider { "Pixel 9" })

    @Test
    fun codeIsNormalized() {
        val cases = listOf(
            "k7q2" to "K7Q2",
            "k7q2-x9" to "K7Q2X9",
            "K7Q2-X9PL-EXTRA" to "K7Q2X9PL",
            " a b!c " to "ABC",
        )
        for ((input, expected) in cases) {
            vm.onCodeChange(input)
            assertEquals(input, expected, vm.state.value.code)
        }
    }

    @Test
    fun submitNeedsCompleteCode() {
        vm.onCodeChange("K7Q2X9P")
        assertFalse(vm.state.value.canSubmit)
        vm.onSubmit()
        assertTrue(auth.registered.isEmpty())
    }

    @Test
    fun successRegistersFormattedCodeAndSignsIn() = runTest {
        auth.registerResult = { testMe(linked = true) }
        vm.events.test {
            vm.onCodeChange("k7q2x9pl")
            vm.onSubmit()
            assertEquals(InviteEvent.SignedIn(telegramLinked = true), awaitItem())
        }
        assertEquals(listOf("K7Q2-X9PL" to "Pixel 9"), auth.registered)
        assertTrue(vm.state.value.submitting)
    }

    @Test
    fun errorIsShownAndClearedOnEdit() {
        val codes = listOf("invalid_invite", "rate_limited", "network")
        for (code in codes) {
            auth.registerResult = { throw quokerException(code) }
            vm.onCodeChange("K7Q2X9PL")
            vm.onSubmit()
            assertEquals(code, vm.state.value.error?.code)
            assertFalse(vm.state.value.submitting)
            vm.onCodeChange("K7Q2X9P")
            assertNull(vm.state.value.error)
        }
    }
}

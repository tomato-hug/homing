package dev.agentpush.poc.ui.theme

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.agentpush.poc.R

object HomingPalette {
    val Background = Color(0xFF0F172A)
    val Card = Color(0xFF1B2336)
    val Raised = Color(0xFF263043)
    val Border = Color(0xFF475569)
    val NavBackground = Color(0xFF0B1222)
    val NavBorder = Color(0xFF1E293B)
    val Text = Color(0xFFF8FAFC)
    val Muted = Color(0xFF94A3B8)
    val Body = Color(0xFFCBD5E1)
    val Brand = Color(0xFF2DD4BF)
    val BrandSoft = Color(0xFF5EEAD4)
    val Warning = Color(0xFFFBBF24)
    val Error = Color(0xFFF87171)
    val Added = Color(0xFF4ADE80)
    val PigeonBody = Color(0xFFE2E8F0)
    val PigeonWing = Color(0xFF94A3B8)
}

private val HomingColors = darkColorScheme(
    primary = HomingPalette.Brand,
    onPrimary = HomingPalette.Background,
    primaryContainer = Color(0xFF134E4A),
    onPrimaryContainer = Color(0xFFCCFBF1),
    secondary = HomingPalette.BrandSoft,
    onSecondary = HomingPalette.Background,
    secondaryContainer = Color(0xFF1E3A3A),
    onSecondaryContainer = Color(0xFFCCFBF1),
    tertiary = HomingPalette.Warning,
    onTertiary = HomingPalette.Background,
    background = HomingPalette.Background,
    onBackground = HomingPalette.Text,
    surface = HomingPalette.Card,
    onSurface = HomingPalette.Text,
    surfaceVariant = HomingPalette.Raised,
    onSurfaceVariant = HomingPalette.Muted,
    outline = HomingPalette.Border,
    outlineVariant = HomingPalette.NavBorder,
    error = HomingPalette.Error,
    errorContainer = Color(0xFF3F1D24),
    onErrorContainer = Color(0xFFFECACA),
    scrim = Color(0xCC020617),
)

val HomingMono = FontFamily(
    Font(R.font.jetbrains_mono_regular, FontWeight.Normal),
    Font(R.font.jetbrains_mono_medium, FontWeight.Medium),
)

private val HomingTypography = Typography(
    displaySmall = TextStyle(
        fontSize = 28.sp,
        lineHeight = 34.sp,
        fontWeight = FontWeight.SemiBold,
        letterSpacing = (-0.2).sp,
    ),
    headlineMedium = TextStyle(
        fontSize = 28.sp,
        lineHeight = 35.sp,
        fontWeight = FontWeight.SemiBold,
        letterSpacing = (-0.3).sp,
    ),
    headlineSmall = TextStyle(
        fontSize = 22.sp,
        lineHeight = 28.sp,
        fontWeight = FontWeight.SemiBold,
    ),
    titleLarge = TextStyle(
        fontSize = 18.sp,
        lineHeight = 24.sp,
        fontWeight = FontWeight.SemiBold,
    ),
    titleMedium = TextStyle(
        fontSize = 16.sp,
        lineHeight = 22.sp,
        fontWeight = FontWeight.SemiBold,
    ),
    titleSmall = TextStyle(
        fontSize = 14.sp,
        lineHeight = 20.sp,
        fontWeight = FontWeight.Medium,
    ),
    bodyLarge = TextStyle(
        fontSize = 16.sp,
        lineHeight = 26.sp,
        fontWeight = FontWeight.Normal,
        letterSpacing = 0.1.sp,
    ),
    bodyMedium = TextStyle(
        fontSize = 14.sp,
        lineHeight = 22.sp,
        fontWeight = FontWeight.Normal,
        letterSpacing = 0.1.sp,
    ),
    bodySmall = TextStyle(
        fontSize = 12.sp,
        lineHeight = 18.sp,
        fontWeight = FontWeight.Normal,
    ),
    labelLarge = TextStyle(
        fontSize = 14.sp,
        lineHeight = 20.sp,
        fontWeight = FontWeight.SemiBold,
    ),
    labelMedium = TextStyle(
        fontSize = 12.sp,
        lineHeight = 17.sp,
        fontWeight = FontWeight.Medium,
    ),
    labelSmall = TextStyle(
        fontSize = 11.sp,
        lineHeight = 15.sp,
        fontWeight = FontWeight.Medium,
    ),
)

private val HomingShapes = Shapes(
    extraSmall = androidx.compose.foundation.shape.RoundedCornerShape(6.dp),
    small = androidx.compose.foundation.shape.RoundedCornerShape(10.dp),
    medium = androidx.compose.foundation.shape.RoundedCornerShape(12.dp),
    large = androidx.compose.foundation.shape.RoundedCornerShape(16.dp),
    extraLarge = androidx.compose.foundation.shape.RoundedCornerShape(22.dp),
)

@Composable
fun AgentInboxTheme(
    darkTheme: Boolean = true,
    content: @Composable () -> Unit,
) {
    MaterialTheme(
        colorScheme = HomingColors,
        typography = HomingTypography,
        shapes = HomingShapes,
        content = content,
    )
}

from django.contrib import admin

from .models import Category, Product, Variant


class VariantInline(admin.TabularInline):
    model = Variant
    extra = 1


@admin.register(Category)
class CategoryAdmin(admin.ModelAdmin):
    search_fields = ["name"]


@admin.register(Product)
class ProductAdmin(admin.ModelAdmin):
    list_display = ["name", "sku", "category", "price", "in_stock", "status", "released"]
    list_filter = ["status", "in_stock", "category"]
    search_fields = ["name", "sku"]
    list_per_page = 10
    date_hierarchy = "released"
    autocomplete_fields = ["category"]
    inlines = [VariantInline]
    actions = ["mark_live"]

    @admin.action(description="Mark selected products as live")
    def mark_live(self, request, queryset):
        queryset.update(status="live")

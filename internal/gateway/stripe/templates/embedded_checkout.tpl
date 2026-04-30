<div id="stripe-checkout-container">
	<div id="stripe-checkout"></div>
</div>
<script src="https://js.stripe.com/dahlia/stripe.js"></script>
<script>
(function() {
	var stripe = globalThis.Stripe('{{.PublishableKey}}');

	async function initialize() {
		var checkout = await stripe.createEmbeddedCheckoutPage({
			fetchClientSecret: async () => '{{.ClientSecret}}'
		});

		checkout.mount('#stripe-checkout');
	}

	window.dispatchEvent(new CustomEvent('paymentCleanupRegister', {
		detail: {
			cleanup: function() {
				var scripts = document.querySelectorAll('script[src*="js.stripe.com"]');
				scripts.forEach(function(s) { s.remove(); });
				delete globalThis.Stripe;
			}
		},
		bubbles: true
	}));

	initialize();
})();
</script>
